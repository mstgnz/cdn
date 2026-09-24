package httpx

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type staticFile struct {
	body         []byte
	contentType  string
	lastModified time.Time
	err          error
}

var (
	staticMu    sync.Mutex
	staticFiles = map[string]*staticFile{}
)

// fileTypes is fasthttp's mapping for the files this service sends.
var fileTypes = map[string]string{
	".png":  "image/png",
	".html": "text/html; charset=utf-8",
}

func loadStatic(path string) *staticFile {
	staticMu.Lock()
	defer staticMu.Unlock()
	if f, ok := staticFiles[path]; ok {
		return f
	}
	f := &staticFile{}
	if info, err := os.Stat(path); err != nil {
		f.err = err
	} else if f.body, f.err = os.ReadFile(path); f.err == nil {
		f.lastModified = info.ModTime().UTC().Truncate(time.Second)
		f.contentType = fileTypes[strings.ToLower(filepath.Ext(path))]
		if f.contentType == "" {
			f.contentType = "application/octet-stream"
		}
	}
	staticFiles[path] = f
	return f
}

// SendFile answers with a file the way fiber's c.SendFile did through
// fasthttp's file server: Last-Modified and If-Modified-Since, single byte
// ranges, and for 304 and 416 a response reset of everything set earlier.
func SendFile(w http.ResponseWriter, r *http.Request, path string) {
	f := loadStatic(path)
	if f.err != nil {
		Text(w, http.StatusNotFound, "sendfile: file "+path+" not found")
		return
	}
	reset := func() {
		if bw := From(w); bw != nil {
			bw.ResetHeaders()
		}
	}

	if ims := r.Header.Get("If-Modified-Since"); ims != "" {
		// fasthttp's ParseHTTPDate accepts RFC 1123 only; anything else is ignored.
		if t, err := time.Parse(time.RFC1123, ims); err == nil && !t.Before(f.lastModified) {
			reset()
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	status, body := http.StatusOK, f.body
	h := w.Header()
	h.Set("Accept-Ranges", "bytes")
	if rng := r.Header.Get("Range"); rng != "" {
		start, end, ok := parseByteRange(rng, len(f.body))
		if !ok {
			reset()
			Text(w, http.StatusRequestedRangeNotSatisfiable, "Range Not Satisfiable")
			return
		}
		h.Set("Content-Range", "bytes "+strconv.Itoa(start)+"-"+strconv.Itoa(end)+"/"+strconv.Itoa(len(f.body)))
		status, body = http.StatusPartialContent, f.body[start:end+1]
	}
	h.Set("Last-Modified", f.lastModified.Format(http.TimeFormat))
	Bytes(w, status, f.contentType, body)
}

// parseByteRange is fasthttp's ParseByteRange: one "bytes=a-b", "a-" or "-n".
func parseByteRange(v string, size int) (int, int, bool) {
	spec, ok := strings.CutPrefix(v, "bytes=")
	if !ok {
		return 0, 0, false
	}
	dash := strings.IndexByte(spec, '-')
	if dash < 0 {
		return 0, 0, false
	}
	if dash == 0 {
		n, err := parseUint(spec[1:])
		if err != nil {
			return 0, 0, false
		}
		return max(size-n, 0), size - 1, true
	}
	start, err := parseUint(spec[:dash])
	if err != nil || start >= size {
		return 0, 0, false
	}
	if spec[dash+1:] == "" {
		return start, size - 1, true
	}
	end, err := parseUint(spec[dash+1:])
	if err != nil {
		return 0, 0, false
	}
	end = min(end, size-1)
	if end < start {
		return 0, 0, false
	}
	return start, end, true
}

func parseUint(s string) (int, error) {
	if s == "" {
		return 0, strconv.ErrSyntax
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, strconv.ErrSyntax
		}
	}
	return strconv.Atoi(s)
}

// SendStream is fiber's c.SendStream(r, size): a known length, then the stream,
// closed afterwards.
func SendStream(w http.ResponseWriter, body io.ReadCloser, size int64) {
	defer body.Close()
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, body)
}

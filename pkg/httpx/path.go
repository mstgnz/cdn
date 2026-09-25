package httpx

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

type ctxKey int

const (
	rawPathKey ctxKey = iota
	formsKey
)

// Prepare records the raw path and routes chi on fiber's detection path. It has
// to run inside the chi mux, where the route context already exists.
//
// It also removes the temp files of every multipart form parsed below it:
// net/http only cleans up the form of the request it created, and handlers
// parse on copies made by WithContext.
func Prepare(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := rawPathOf(r)
		if rctx := chi.RouteContext(r.Context()); rctx != nil {
			rctx.RoutePath = DetectionPath(raw)
		}
		forms := &parsedForms{}
		defer forms.removeAll()
		ctx := context.WithValue(context.WithValue(r.Context(), rawPathKey, raw), formsKey, forms)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RawPath is the path exactly as the client sent it, undecoded, which is what
// fiber matched against and handed to handlers (fasthttp's PathOriginal).
func RawPath(r *http.Request) string {
	if raw, ok := r.Context().Value(rawPathKey).(string); ok {
		return raw
	}
	return rawPathOf(r)
}

func rawPathOf(r *http.Request) string {
	uri := r.RequestURI
	if strings.HasPrefix(uri, "/") {
		if i := strings.IndexByte(uri, '?'); i >= 0 {
			uri = uri[:i]
		}
		return uri
	}
	return r.URL.EscapedPath()
}

// DetectionPath is fiber's routing view of a path: ASCII lower-cased and without
// trailing slashes. Its length equals the raw path's minus those slashes, which
// is what lets Param slice values out of the raw path.
func DetectionPath(raw string) string {
	b := []byte(raw)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	if len(b) > 1 && b[len(b)-1] == '/' {
		b = []byte(strings.TrimRight(string(b), "/"))
	}
	return string(b)
}

// Param returns a route parameter as fiber's c.Params did: original case and
// encoding, with trailing slashes left out of a trailing wildcard. "*" names the
// wildcard; a parameter the matched route does not have is "".
func Param(r *http.Request, name string) string {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil {
		return ""
	}
	raw := RawPath(r)
	start, end, ok := paramSpan(rctx.RoutePattern(), DetectionPath(raw), name)
	if !ok {
		return ""
	}
	return raw[start:end]
}

// paramSpan walks a chi pattern over the detection path and reports where the
// named parameter sits. Patterns here only use literals, "{name}" ending at the
// next slash, and a final "*".
func paramSpan(pattern, detect, name string) (int, int, bool) {
	pos := 0
	for i := 0; i < len(pattern); {
		switch {
		case pattern[i] == '*':
			return pos, len(detect), name == "*"
		case pattern[i] == '{':
			closing := strings.IndexByte(pattern[i:], '}')
			if closing < 0 {
				return 0, 0, false
			}
			param := pattern[i+1 : i+closing]
			end := strings.IndexByte(detect[pos:], '/')
			if end < 0 {
				end = len(detect)
			} else {
				end += pos
			}
			if param == name {
				return pos, end, true
			}
			pos, i = end, i+closing+1
		default:
			next := strings.IndexAny(pattern[i:], "{*")
			if next < 0 {
				next = len(pattern) - i
			}
			literal := pattern[i : i+next]
			switch {
			case strings.HasPrefix(detect[pos:], literal):
				pos += len(literal)
			case strings.HasSuffix(literal, "/") && detect[pos:] == literal[:len(literal)-1]:
				// fiber's optional slash before a wildcard: "/b" matches "/:bucket/*".
				pos += len(literal) - 1
			default:
				return 0, 0, false
			}
			i += next
		}
	}
	return 0, 0, false
}

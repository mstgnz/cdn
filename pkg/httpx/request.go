package httpx

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
)

// multipartMemory is how much of a multipart body stays in memory before
// net/http spills file parts to disk; fasthttp kept everything in memory.
const multipartMemory = 32 << 20

// ErrUnprocessableEntity is what fiber's BodyParser returned for a content
// type it could not bind.
var ErrUnprocessableEntity = errors.New("Unprocessable Entity")

// Query returns the first value of a query argument, as fasthttp's Peek did.
func Query(r *http.Request, key string) string {
	return r.URL.Query().Get(key)
}

// FormValue looks up a value where fasthttp did, in this order: query string,
// urlencoded body, multipart body. An empty value falls through to the next.
func FormValue(r *http.Request, key string) string {
	if v := r.URL.Query()[key]; len(v) > 0 && v[0] != "" {
		return v[0]
	}
	if isURLEncoded(r) {
		if v := postArgs(r)[key]; len(v) > 0 && v[0] != "" {
			return v[0]
		}
	}
	if form, err := MultipartForm(r); err == nil {
		if v := form.Value[key]; len(v) > 0 {
			return v[0]
		}
	}
	return ""
}

// MultipartForm parses the body once; net/http removes spilled files after the
// handler returns.
func MultipartForm(r *http.Request) (*multipart.Form, error) {
	if r.MultipartForm == nil {
		if err := r.ParseMultipartForm(multipartMemory); err != nil {
			return nil, err
		}
	}
	return r.MultipartForm, nil
}

// FormFile returns the first file part under key.
func FormFile(r *http.Request, key string) (*multipart.FileHeader, error) {
	form, err := MultipartForm(r)
	if err != nil {
		return nil, err
	}
	if files := form.File[key]; len(files) > 0 {
		return files[0], nil
	}
	return nil, http.ErrMissingFile
}

func mediaType(r *http.Request) string {
	ct := strings.ToLower(r.Header.Get("Content-Type"))
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	return strings.TrimSpace(ct)
}

func isURLEncoded(r *http.Request) bool {
	return mediaType(r) == "application/x-www-form-urlencoded"
}

// postArgs is fasthttp's PostArgs: an urlencoded body read for any method.
// net/http's ParseForm skips the body of a DELETE, which is how batch delete
// arrives. Parsed once and kept in r.PostForm.
func postArgs(r *http.Request) url.Values {
	if r.PostForm == nil {
		body, _ := io.ReadAll(r.Body)
		values, _ := url.ParseQuery(string(body)) // well-formed pairs survive a bad one
		r.PostForm = values
	}
	return r.PostForm
}

// BindBody is fiber's BodyParser: JSON for any "+json"/"json" type, form
// fields for urlencoded and multipart bodies, XML, and an error otherwise.
func BindBody(r *http.Request, out any) error {
	ctype := mediaType(r)
	// fiber's ParseVendorSpecificContentType: application/vnd.x+json is json.
	if slash := strings.IndexByte(ctype, '/'); slash >= 0 {
		if plus := strings.LastIndexByte(ctype, '+'); plus > slash {
			ctype = ctype[:slash+1] + ctype[plus+1:]
		}
	}
	switch {
	case strings.HasSuffix(ctype, "json"):
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return err
		}
		return json.Unmarshal(body, out)
	case ctype == "application/x-www-form-urlencoded":
		return bindForm(out, postArgs(r))
	case ctype == "multipart/form-data":
		form, err := MultipartForm(r)
		if err != nil {
			return err
		}
		return bindForm(out, form.Value)
	case ctype == "text/xml" || ctype == "application/xml":
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return err
		}
		if err := xml.Unmarshal(body, out); err != nil {
			return fmt.Errorf("failed to unmarshal: %w", err)
		}
		return nil
	}
	return ErrUnprocessableEntity
}

// bindForm is the part of fiber's schema decoder the request structs use:
// field matched by `form` tag or name, case-insensitively; scalars take the
// last value; empty means zero; "files[]" is "files"; unknown keys are ignored.
func bindForm(out any, values map[string][]string) error {
	v := reflect.ValueOf(out)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return errors.New("schema: interface must be a pointer to struct")
	}
	v = v.Elem()
	t := v.Type()

	data := make(map[string][]string, len(values))
	for key, vals := range values {
		k, err := squareBrackets(key)
		if err != nil {
			return err
		}
		data[k] = append(data[k], vals...)
	}

	for key, vals := range data {
		for i := 0; i < t.NumField(); i++ {
			sf := t.Field(i)
			alias := sf.Tag.Get("form")
			if alias == "" {
				alias = sf.Name
			}
			if !sf.IsExported() || !strings.EqualFold(alias, key) {
				continue
			}
			if err := setField(v.Field(i), vals); err != nil {
				return fmt.Errorf("failed to decode: schema: error converting value for %q", key)
			}
			break
		}
	}
	return nil
}

func setField(f reflect.Value, vals []string) error {
	if f.Kind() == reflect.Pointer {
		if f.IsNil() {
			f.Set(reflect.New(f.Type().Elem()))
		}
		f = f.Elem()
	}
	if f.Kind() == reflect.Slice {
		out := reflect.MakeSlice(f.Type(), 0, len(vals))
		for _, s := range vals {
			item := reflect.New(f.Type().Elem()).Elem()
			if s != "" {
				if err := setScalar(item, s); err != nil {
					return err
				}
			}
			out = reflect.Append(out, item)
		}
		f.Set(out)
		return nil
	}
	last := ""
	if len(vals) > 0 {
		last = vals[len(vals)-1]
	}
	if last == "" {
		f.Set(reflect.Zero(f.Type()))
		return nil
	}
	return setScalar(f, last)
}

func setScalar(f reflect.Value, s string) error {
	switch f.Kind() {
	case reflect.String:
		f.SetString(s)
	case reflect.Bool:
		if s == "on" {
			f.SetBool(true)
			return nil
		}
		b, err := strconv.ParseBool(s)
		if err != nil {
			return err
		}
		f.SetBool(b)
	default:
		return fmt.Errorf("schema: converter not found for %v", f.Type())
	}
	return nil
}

// squareBrackets is fiber's parseParamSquareBrackets: "a[b]" is "a.b", "a[]" is "a".
func squareBrackets(k string) (string, error) {
	if !strings.Contains(k, "[") {
		return k, nil
	}
	var b strings.Builder
	open := 0
	for i := 0; i < len(k); i++ {
		switch c := k[i]; c {
		case '[':
			open++
			if i+1 < len(k) && k[i+1] != ']' {
				b.WriteByte('.')
			}
		case ']':
			open--
			if open < 0 {
				return "", errors.New("unmatched brackets")
			}
		default:
			b.WriteByte(c)
		}
	}
	if open > 0 {
		return "", errors.New("unmatched brackets")
	}
	return b.String(), nil
}

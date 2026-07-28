package jsonx

import "net/http"

const contentTypeJSON = "application/json; charset=utf-8"

// GinRenderer is a drop-in replacement for gin/render.JSON that serialises
// Data using the build-tag-selected JSON implementation (sonic on amd64/arm64
// when compiled with -tags=sonic, encoding/json everywhere else).
//
// Usage in a Gin handler:
//
//	c.Render(http.StatusOK, jsonx.GinRenderer{Data: payload})
//
// This is the only change needed in the hot handler paths: the Content-Type
// header, status code, and error surfacing all behave identically to c.JSON().
type GinRenderer struct {
	Data any
}

// Render writes the JSON-encoded Data to w and returns any encoding error.
func (r GinRenderer) Render(w http.ResponseWriter) error {
	r.WriteContentType(w)
	enc := NewEncoder(w)
	return enc.Encode(r.Data)
}

// WriteContentType sets Content-Type to application/json without writing the body.
func (r GinRenderer) WriteContentType(w http.ResponseWriter) {
	header := w.Header()
	if val := header["Content-Type"]; len(val) == 0 {
		header["Content-Type"] = []string{contentTypeJSON}
	}
}

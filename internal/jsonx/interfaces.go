// Package jsonx provides a thin, build-tag-selected JSON wrapper.
// Use jsonx.Marshal / jsonx.Unmarshal / jsonx.NewEncoder / jsonx.NewDecoder
// in place of encoding/json on hot handler paths.
//
// Compile with -tags=sonic on amd64/arm64 to activate bytedance/sonic.
// All other configurations fall back to encoding/json automatically.
//
// # Architecture / build-tag matrix
//
//	Tag      GOARCH        Implementation
//	sonic    amd64/arm64   bytedance/sonic  (SIMD-accelerated)
//	<none>   any           encoding/json    (stdlib fallback)
//	sonic    386/mips/…    encoding/json    (tag ignored, unsupported arch)
//
// # Security notes
//
//   - bytedance/sonic does not HTML-escape <, >, & by default. For
//     application/json HTTP responses this is correct: the HTTP
//     Content-Type boundary prevents XSS without escaping. If you ever
//     embed JSON inside HTML, call SetEscapeHTML(true) on the encoder or
//     use the stdlib fallback.
//   - Both implementations handle untrusted input safely: Unmarshal /
//     Decode do not execute arbitrary code and are bounded by the input
//     size enforced upstream by request-size middleware.
package jsonx

import "io"

// Encoder is the common interface satisfied by both sonic and encoding/json
// streaming encoders.
type Encoder interface {
	Encode(v any) error
	SetIndent(prefix, indent string)
	SetEscapeHTML(on bool)
}

// Decoder is the common interface satisfied by both sonic and encoding/json
// streaming decoders.
type Decoder interface {
	Decode(v any) error
	More() bool
	UseNumber()
	DisallowUnknownFields()
	Buffered() io.Reader
}

//go:build sonic && (amd64 || arm64)

// Package jsonx is a thin drop-in wrapper around a JSON encoder/decoder.
// When the "sonic" build tag is set on a supported architecture (amd64,
// arm64) it delegates to bytedance/sonic, which is a zero-copy,
// SIMD-accelerated JSON library that reduces per-request CPU on
// high-QPS list handlers.
//
// On every other architecture or when the "sonic" tag is absent the
// stdlib fallback (jsonx_stdlib.go) is compiled instead, keeping the
// API surface identical.
//
// # Build matrix
//
//	Tag      GOARCH        Implementation
//	sonic    amd64/arm64   bytedance/sonic  (this file)
//	<none>   any           encoding/json    (jsonx_stdlib.go)
//	sonic    386/mips/…    encoding/json    (jsonx_stdlib.go — tag ignored)
//
// # Usage
//
//	import "stellarbill-backend/internal/jsonx"
//
//	data, err := jsonx.Marshal(v)
//	err        = jsonx.Unmarshal(data, &v)
//	err        = jsonx.NewEncoder(w).Encode(v)
//	v, err    := jsonx.NewDecoder(r).Decode(&v)
package jsonx

import (
	"io"

	"github.com/bytedance/sonic"
)

// Marshal serialises v to JSON using sonic.
// HTML-special characters (<, >, &) are NOT automatically escaped by sonic;
// callers that embed the output in HTML contexts must sanitise separately.
// For API JSON responses over HTTP this is the correct default: the HTTP
// Content-Type is application/json, not text/html, so escaping is not needed.
func Marshal(v any) ([]byte, error) {
	return sonic.Marshal(v)
}

// Unmarshal parses the JSON-encoded data and stores the result in v.
func Unmarshal(data []byte, v any) error {
	return sonic.Unmarshal(data, v)
}

// MarshalIndent is the indented-output analogue of Marshal.
func MarshalIndent(v any, prefix, indent string) ([]byte, error) {
	return sonic.MarshalIndent(v, prefix, indent)
}

// NewEncoder returns a streaming JSON encoder that writes to w.
func NewEncoder(w io.Writer) Encoder {
	return sonic.ConfigDefault.NewEncoder(w)
}

// NewDecoder returns a streaming JSON decoder that reads from r.
func NewDecoder(r io.Reader) Decoder {
	return sonic.ConfigDefault.NewDecoder(r)
}

// ConfigName reports which implementation is active. Useful in tests and
// startup diagnostics.
func ConfigName() string { return "sonic" }

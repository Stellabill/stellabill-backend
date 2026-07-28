//go:build !sonic || !(amd64 || arm64)

// Package jsonx is a thin drop-in wrapper around encoding/json.
// This file is compiled when the "sonic" build tag is not set, or when the
// target architecture is not amd64/arm64 (i.e. sonic is not supported).
// The API is identical to jsonx_sonic.go so callers are architecture-agnostic.
package jsonx

import (
	"encoding/json"
	"io"
)

// Marshal serialises v to JSON using encoding/json.
// HTML-special characters are escaped (stdlib default behaviour).
func Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

// Unmarshal parses the JSON-encoded data and stores the result in v.
func Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// MarshalIndent is the indented-output analogue of Marshal.
func MarshalIndent(v any, prefix, indent string) ([]byte, error) {
	return json.MarshalIndent(v, prefix, indent)
}

// NewEncoder returns a streaming JSON encoder that writes to w.
func NewEncoder(w io.Writer) Encoder {
	return json.NewEncoder(w)
}

// NewDecoder returns a streaming JSON decoder that reads from r.
func NewDecoder(r io.Reader) Decoder {
	return json.NewDecoder(r)
}

// ConfigName reports which implementation is active. Useful in tests and
// startup diagnostics.
func ConfigName() string { return "stdlib" }

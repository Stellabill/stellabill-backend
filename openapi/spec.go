package openapi

import (
	_ "embed"

	"github.com/getkin/kin-openapi/openapi3"
)

//go:embed openapi.yaml
var specYAML []byte

func Load() (*openapi3.T, error) {
	return loadFromData(specYAML)
}

func loadFromData(data []byte) (*openapi3.T, error) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(data)
	if err != nil {
		return nil, err
	}
	if err := doc.Validate(loader.Context); err != nil {
		return nil, err
	}
	return doc, nil
}

// LoadFromBytes parses and validates an OpenAPI document from a raw byte slice.
// This is intended for tooling (e.g. openapi-lint) that reads the spec from an
// arbitrary file path rather than the embedded default.
func LoadFromBytes(data []byte) (*openapi3.T, error) {
	return loadFromData(data)
}

// LoadFromBytesRaw parses an OpenAPI document without running schema-level
// validation (e.g. example validation).  This is useful in tests that
// deliberately construct documents with invalid examples to verify lint logic.
func LoadFromBytesRaw(data []byte) (*openapi3.T, error) {
	loader := openapi3.NewLoader()
	return loader.LoadFromData(data)
}

func RawYAML() []byte {
	return specYAML
}

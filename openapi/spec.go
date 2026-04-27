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

func RawYAML() []byte {
	return specYAML
}


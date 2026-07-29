package handlers

import (
	"encoding/json"
	"fmt"
)

// ProjectFields takes a struct value and a set of field names to include,
// then returns a map that can be serialized to JSON containing only the
// requested fields.
//
// The projection is performed at the JSON level: the input is marshalled
// to JSON, then unmarshalled into a map[string]json.RawMessage, and only
// the entries matching the requested field names are kept.
//
// This approach avoids reflection-based field access and works correctly
// with custom MarshalJSON implementations, omitempty, and nested structs.
//
// fields must be a non-empty subset of the JSON key names that the struct
// produces. Invalid field names are silently ignored (validation is the
// caller's responsibility, typically via requestparams.ParseFields).
func ProjectFields(v any, fields []string) (map[string]json.RawMessage, error) {
	if len(fields) == 0 {
		return nil, fmt.Errorf("fields must not be empty")
	}

	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal for projection: %w", err)
	}

	var full map[string]json.RawMessage
	if err := json.Unmarshal(raw, &full); err != nil {
		return nil, fmt.Errorf("unmarshal for projection: %w", err)
	}

	projected := make(map[string]json.RawMessage, len(fields))
	for _, name := range fields {
		if val, ok := full[name]; ok {
			projected[name] = val
		}
	}

	return projected, nil
}

// ProjectSlice applies ProjectFields to each element of a slice and returns
// a slice of projected maps. This is useful for list endpoints.
func ProjectSlice[T any](items []T, fields []string) ([]map[string]json.RawMessage, error) {
	result := make([]map[string]json.RawMessage, 0, len(items))
	for _, item := range items {
		p, err := ProjectFields(item, fields)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, nil
}

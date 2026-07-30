package config

import (
	"fmt"
	"reflect"
	"strings"
)

const redacted = "***REDACTED***"

// ConfigDump represents the redacted runtime configuration for diagnostics.
// Secret fields tagged with `secret:"true"` are replaced with a redacted
// placeholder before exposure.
type ConfigDump map[string]interface{}

// Dump returns a redacted snapshot of the configuration.
//
// Each field of the Config struct is serialized into the output map using its
// `json` tag as the key. Fields whose struct tag includes `secret:"true"` are
// replaced with the constant redacted placeholder "***REDACTED***".
//
// Nested structs are traversed recursively and any secret-annotated fields
// within them are redacted as well.
//
// The output is suitable for on-call diagnostics endpoints and admin-only
// inspection tools.
func Dump(cfg *Config) ConfigDump {
	return dumpValue(reflect.ValueOf(cfg).Elem()).(ConfigDump)
}

// dumpValue recursively converts a reflected value into a dump-safe
// representation, redacting fields tagged secret:"true".
func dumpValue(v reflect.Value) interface{} {
	switch v.Kind() {
	case reflect.Struct:
		return dumpStruct(v)
	case reflect.Ptr:
		if v.IsNil() {
			return nil
		}
		return dumpValue(v.Elem())
	case reflect.Map:
		return dumpMap(v)
	case reflect.Slice:
		return dumpSlice(v)
	case reflect.Array:
		return dumpSlice(v)
	case reflect.String:
		return v.String()
	case reflect.Bool:
		return v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint()
	case reflect.Float32, reflect.Float64:
		return v.Float()
	default:
		return fmt.Sprintf("%v", v.Interface())
	}
}

// dumpStruct converts a struct value to a ConfigDump, redacting fields
// tagged with `secret:"true"`.
func dumpStruct(v reflect.Value) ConfigDump {
	t := v.Type()
	out := make(ConfigDump, t.NumField())

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fieldVal := v.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Resolve the output key from the json tag, falling back to the
		// field name in snake_case.
		key := jsonTagName(field)
		if key == "" || key == "-" {
			continue
		}

		// If the field is tagged secret:"true", redact it immediately
		// without recursing into the value.
		if isSecretField(field) {
			out[key] = redacted
			continue
		}

		out[key] = dumpValue(fieldVal)
	}

	return out
}

// dumpMap converts a map value to a dump-safe representation.
func dumpMap(v reflect.Value) interface{} {
	out := make(map[string]interface{}, v.Len())
	for _, key := range v.MapKeys() {
		kStr := fmt.Sprintf("%v", key.Interface())
		out[kStr] = dumpValue(v.MapIndex(key))
	}
	return out
}

// dumpSlice converts a slice or array value to a dump-safe representation.
func dumpSlice(v reflect.Value) []interface{} {
	n := v.Len()
	out := make([]interface{}, n)
	for i := 0; i < n; i++ {
		out[i] = dumpValue(v.Index(i))
	}
	return out
}

// jsonTagName extracts the JSON key name from a struct field's json tag.
// It returns the tag name without options (before the first comma).
func jsonTagName(field reflect.StructField) string {
	tag, ok := field.Tag.Lookup("json")
	if !ok {
		return snakeCase(field.Name)
	}
	if idx := strings.Index(tag, ","); idx >= 0 {
		return tag[:idx]
	}
	return tag
}

// isSecretField checks whether a struct field has a `secret:"true"` tag.
func isSecretField(field reflect.StructField) bool {
	tag, ok := field.Tag.Lookup("secret")
	if !ok {
		return false
	}
	return tag == "true"
}

// snakeCase converts a CamelCase name to snake_case.
func snakeCase(s string) string {
	var result strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				result.WriteRune('_')
			}
			result.WriteRune(r + ('a' - 'A'))
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}

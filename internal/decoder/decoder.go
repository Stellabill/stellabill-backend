// Package decoder provides strict JSON decoding for mutation endpoints.
// It rejects unknown fields and enforces strict type matching to prevent
// accidental API misuse and parsing ambiguity.
package decoder

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

// ErrUnknownField is returned when the request body contains a field not
// defined in the target struct.
var ErrUnknownField = errors.New("unknown field in request body")

// DecodeStrict reads the request body into dst using strict decoding:
//   - Unknown fields cause a 400 error (prevents silent data loss / typo bugs).
//   - Type mismatches cause a 400 error.
//   - A single JSON object is expected (no trailing data).
//
// Returns nil on success. On failure it writes the error response and returns
// a non-nil error so the caller can return immediately.
func DecodeStrict(c *gin.Context, dst interface{}) error {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		respondDecodeError(c, http.StatusBadRequest, "INVALID_BODY", "failed to read request body")
		return err
	}
	// Restore body for any downstream middleware that may re-read it.
	c.Request.Body = io.NopCloser(bytes.NewReader(body))

	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		var syntaxErr *json.SyntaxError
		var unmarshalTypeErr *json.UnmarshalTypeError

		switch {
		case errors.As(err, &syntaxErr):
			respondDecodeError(c, http.StatusBadRequest, "INVALID_JSON",
				fmt.Sprintf("malformed JSON at position %d", syntaxErr.Offset))
		case errors.As(err, &unmarshalTypeErr):
			respondDecodeError(c, http.StatusBadRequest, "INVALID_FIELD_TYPE",
				fmt.Sprintf("field %q: expected %s", unmarshalTypeErr.Field, unmarshalTypeErr.Type))
		case isUnknownFieldError(err):
			respondDecodeError(c, http.StatusBadRequest, "UNKNOWN_FIELD", err.Error())
		case errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF):
			respondDecodeError(c, http.StatusBadRequest, "INVALID_JSON", "empty or incomplete request body")
		default:
			respondDecodeError(c, http.StatusBadRequest, "INVALID_JSON", err.Error())
		}
		return err
	}

	// Reject trailing data after the first JSON value.
	if dec.More() {
		respondDecodeError(c, http.StatusBadRequest, "INVALID_JSON", "request body must contain exactly one JSON object")
		return errors.New("trailing data after JSON object")
	}

	return nil
}

// isUnknownFieldError detects the error produced by DisallowUnknownFields.
// The standard library returns a plain *json.UnmarshalTypeError or a string
// error starting with "json: unknown field".
func isUnknownFieldError(err error) bool {
	if err == nil {
		return false
	}
	// encoding/json formats unknown-field errors as:
	//   json: unknown field "fieldName"
	return len(err.Error()) > 21 && err.Error()[:21] == "json: unknown field \""
}

// DecodeErrorResponse is the JSON shape returned for all strict-decode failures.
type DecodeErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func respondDecodeError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, DecodeErrorResponse{Code: code, Message: message})
}

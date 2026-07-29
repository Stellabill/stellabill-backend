package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"strings"

	"stellarbill-backend/internal/validator"
)

func decodeJSONPatchPayload(r io.Reader, allowedFields []string) (map[string]json.RawMessage, error) {
	var payload map[string]json.RawMessage
	decoder := json.NewDecoder(r)
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("invalid request body: %w", err)
	}
	if err := validator.ValidateMergePatch(payload, allowedFields); err != nil {
		return nil, err
	}
	return payload, nil
}

func parseMediaType(contentType string) (string, error) {
	if strings.TrimSpace(contentType) == "" {
		return "application/json", nil
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", fmt.Errorf("unsupported content type %q", contentType)
	}
	return mediaType, nil
}

func decodePatchStringValue(raw json.RawMessage) (string, bool, error) {
	if raw == nil {
		return "", false, nil
	}
	if string(raw) == "null" {
		return "", true, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false, fmt.Errorf("invalid string value: %w", err)
	}
	return value, true, nil
}

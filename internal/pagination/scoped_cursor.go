package pagination

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
)

// ScopedCursor extends Cursor with a TenantID so that cursors cannot be reused
// across tenants. The encoded form includes an HMAC signature to prevent
// tampering with the tenant scope.
type ScopedCursor struct {
	ID        string `json:"id"`
	SortValue string `json:"sort_value,omitempty"`
	TenantID  string `json:"tenant_id"`
	Sig       string `json:"sig"`
}

func cursorSecret() []byte {
	if s := os.Getenv("CURSOR_HMAC_SECRET"); s != "" {
		return []byte(s)
	}
	return []byte("stellabill-cursor-hmac-default")
}

func signCursor(id, sortValue, tenantID string) string {
	mac := hmac.New(sha256.New, cursorSecret())
	mac.Write([]byte(id + "|" + sortValue + "|" + tenantID))
	return hex.EncodeToString(mac.Sum(nil))
}

func EncodeScopedCursor(id, sortValue, tenantID string) string {
	if id == "" && sortValue == "" {
		return ""
	}
	sc := ScopedCursor{
		ID:        id,
		SortValue: sortValue,
		TenantID:  tenantID,
		Sig:       signCursor(id, sortValue, tenantID),
	}
	b, err := json.Marshal(sc)
	if err != nil {
		return ""
	}
	return base64.URLEncoding.EncodeToString(b)
}

// DecodeScopedCursor decodes a cursor and validates that it belongs to the
// expected tenant. Returns an error if the cursor was issued for a different
// tenant or has been tampered with.
func DecodeScopedCursor(encoded string, expectedTenantID string) (Cursor, error) {
	if encoded == "" {
		return Cursor{}, nil
	}

	b, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		return Cursor{}, errors.New("invalid cursor format")
	}

	var sc ScopedCursor
	if err := json.Unmarshal(b, &sc); err != nil {
		return Cursor{}, errors.New("invalid cursor format")
	}

	expected := signCursor(sc.ID, sc.SortValue, sc.TenantID)
	if !hmac.Equal([]byte(sc.Sig), []byte(expected)) {
		return Cursor{}, errors.New("cursor signature invalid")
	}

	if sc.TenantID != expectedTenantID {
		return Cursor{}, errors.New("cursor does not belong to this tenant")
	}

	return Cursor{ID: sc.ID, SortValue: sc.SortValue}, nil
}

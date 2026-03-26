package pagination

import (
	"encoding/base64"
	"encoding/json"
	"errors"
)

// Cursor represents the pointer to a specific record for pagination.
// It supports a generic ID (for primary sorting/tie-breaking) and an optional
// SortValue if sorting by a secondary column (to handle duplicate sort keys).
type Cursor struct {
	ID        string `json:"id"`
	SortValue string `json:"sort_value,omitempty"`
}

// Encode serializes a Cursor struct into an opaque base64 string.
// If the cursor is entirely empty, it returns an empty string.
func Encode(c Cursor) string {
	if c.ID == "" && c.SortValue == "" {
		return ""
	}
	b, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return base64.URLEncoding.EncodeToString(b)
}

// Decode deserializes an opaque base64 string back into a Cursor struct.
// Returns an empty cursor if the input string is empty.
func Decode(s string) (Cursor, error) {
	var c Cursor
	if s == "" {
		return c, nil
	}

	b, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return c, errors.New("invalid cursor format")
	}

	if err := json.Unmarshal(b, &c); err != nil {
		return c, errors.New("invalid cursor format")
	}

	return c, nil
}

// Item metadata extractor for pagination.
type Item interface {
	GetID() string
	GetSortValue() string
}

// PaginateSlice simulates cursor-based pagination over an in-memory slice.
// It assumes the slice is ALREADY SORTED by (SortValue ASC, ID ASC).
// This is used to test cursor continuity, stale records, and duplicate keys.
func PaginateSlice[T Item](items []T, cursor Cursor, limit int) ([]T, Cursor, bool) {
	if limit <= 0 {
		limit = 10
	}

	startIdx := 0
	if cursor.ID != "" || cursor.SortValue != "" {
		// Find the first item that comes AFTER the cursor.
		// For ASC sorting:
		// item.SortValue > cursor.SortValue OR (item.SortValue == cursor.SortValue AND item.ID > cursor.ID)
		found := false
		for i, item := range items {
			sv := item.GetSortValue()
			id := item.GetID()

			if sv > cursor.SortValue || (sv == cursor.SortValue && id > cursor.ID) {
				startIdx = i
				found = true
				break
			}
		}

		// If we gave a cursor but couldn't find any items after it (or it was stale and all subsequent were deleted),
		// we return empty.
		if !found {
			return []T{}, Cursor{}, false
		}
	}

	endIdx := startIdx + limit
	hasMore := true
	if endIdx >= len(items) {
		endIdx = len(items)
		hasMore = false
	}

	page := items[startIdx:endIdx]

	var nextCursor Cursor
	if len(page) > 0 && hasMore {
		lastItem := page[len(page)-1]
		nextCursor = Cursor{
			ID:        lastItem.GetID(),
			SortValue: lastItem.GetSortValue(),
		}
	}

	return page, nextCursor, hasMore
}

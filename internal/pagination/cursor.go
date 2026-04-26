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

// Page represents a standardized paginated response envelope.
type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

// PaginateSlice simulates cursor-based pagination over an in-memory slice.
// It assumes the slice is ALREADY SORTED by (SortValue ASC, ID ASC).
func PaginateSlice[T Item](items []T, cursor Cursor, limit int) Page[T] {
	if limit <= 0 {
		limit = 10
	}

	startIdx := 0
	if cursor.ID != "" || cursor.SortValue != "" {
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

		if !found {
			return Page[T]{
				Items:   []T{},
				HasMore: false,
			}
		}
	}

	endIdx := startIdx + limit
	hasMore := true
	if endIdx >= len(items) {
		endIdx = len(items)
		hasMore = false
	}

	pageItems := items[startIdx:endIdx]

	var nextCursorStr string
	if len(pageItems) > 0 && hasMore {
		lastItem := pageItems[len(pageItems)-1]
		nextCursorStr = Encode(Cursor{
			ID:        lastItem.GetID(),
			SortValue: lastItem.GetSortValue(),
		})
	}

	return Page[T]{
		Items:      pageItems,
		NextCursor: nextCursorStr,
		HasMore:    hasMore,
	}
}


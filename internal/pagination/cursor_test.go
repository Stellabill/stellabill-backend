package pagination

import (
	"testing"
)

func TestEncodeDecode(t *testing.T) {
	c := Cursor{ID: "usr_123", SortValue: "100"}
	encoded := Encode(c)
	if encoded == "" {
		t.Fatal("expected encoded string, got empty")
	}

	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decoded.ID != c.ID {
		t.Errorf("expected ID %s, got %s", c.ID, decoded.ID)
	}
	if decoded.SortValue != c.SortValue {
		t.Errorf("expected SortValue %s, got %s", c.SortValue, decoded.SortValue)
	}
}

func TestDecodeEmpty(t *testing.T) {
	c, err := Decode("")
	if err != nil {
		t.Fatalf("unexpected err for empty base64: %v", err)
	}
	if c.ID != "" || c.SortValue != "" {
		t.Error("expected empty cursor elements")
	}

	encoded := Encode(Cursor{})
	if encoded != "" {
		t.Errorf("expected empty string for empty cursor, got %s", encoded)
	}
}

func TestDecodeInvalid(t *testing.T) {
	// Invalid base64
	_, err := Decode("invalid-base64-!@#")
	if err == nil {
		t.Error("expected error for invalid base64")
	}

	// Valid base64 but invalid json
	encodedInvalidJson := "bm90LWpzb24=" // "not-json" base64
	_, err = Decode(encodedInvalidJson)
	if err == nil {
		t.Error("expected error for invalid json")
	}
}

// Using dummy struct for tests
type dummyItem struct {
	id  string
	val string
}

func (d dummyItem) GetID() string        { return d.id }
func (d dummyItem) GetSortValue() string { return d.val }

func TestPaginateSlice_Empty(t *testing.T) {
	items := []dummyItem{}
	page, next, hasMore := PaginateSlice(items, Cursor{}, 10)
	if len(page) != 0 {
		t.Errorf("expected 0 items, got %d", len(page))
	}
	if hasMore {
		t.Error("expected hasMore false")
	}
	if next.ID != "" {
		t.Error("expected empty next cursor")
	}
}

func TestPaginateSlice_ContinuityAndDuplicates(t *testing.T) {
	// Items are sorted by val ASC, id ASC
	items := []dummyItem{
		{"a", "10"},
		{"b", "10"}, // duplicate val
		{"c", "20"},
		{"d", "30"},
		{"e", "30"}, // duplicate val
		{"f", "40"},
		{"g", "50"},
	}

	// Page 1: limit 2
	page1, nextCursor1, hasMore := PaginateSlice(items, Cursor{}, 2)
	if len(page1) != 2 || page1[0].id != "a" || page1[1].id != "b" {
		t.Errorf("page1 incorrect: %v", page1)
	}
	if !hasMore {
		t.Error("expected hasMore true")
	}
	if nextCursor1.ID != "b" || nextCursor1.SortValue != "10" {
		t.Errorf("expected next1 to be b:10, got %v", nextCursor1)
	}

	// Page 2: limit 2
	page2, nextCursor2, hasMore2 := PaginateSlice(items, nextCursor1, 2)
	if len(page2) != 2 || page2[0].id != "c" || page2[1].id != "d" {
		t.Errorf("page2 incorrect: %v", page2)
	}
	if !hasMore2 {
		t.Error("expected hasMore true")
	}
	if nextCursor2.ID != "d" || nextCursor2.SortValue != "30" {
		t.Errorf("expected next2 to be d:30, got %v", nextCursor2)
	}

	// Page 3: limit 5 (over limits)
	page3, nextCursor3, hasMore3 := PaginateSlice(items, nextCursor2, 5)
	if len(page3) != 3 || page3[0].id != "e" || page3[1].id != "f" || page3[2].id != "g" {
		t.Errorf("page3 incorrect: %v", page3)
	}
	if hasMore3 {
		t.Error("expected hasMore false")
	}
	if nextCursor3.ID != "" {
		t.Errorf("expected empty next3 cursor, got %v", nextCursor3)
	}
}

func TestPaginateSlice_StaleCursor(t *testing.T) {
	// A cursor points to an item that doesn't exist, but it should still return
	// items that logically come AFTER it.
	items := []dummyItem{
		{"a", "10"},
		{"c", "30"},
		{"e", "50"},
	}

	// Cursor for a deleted "b" with val "20"
	cursor := Cursor{ID: "b", SortValue: "20"}
	page, _, _ := PaginateSlice(items, cursor, 2)

	// Should fetch c and e since their sort value is > 20
	if len(page) != 2 || page[0].id != "c" || page[1].id != "e" {
		t.Errorf("stale cursor pagination failed. got: %v", page)
	}
}

func TestPaginateSlice_CursorBeyondData(t *testing.T) {
	items := []dummyItem{
		{"a", "10"},
	}

	cursor := Cursor{ID: "z", SortValue: "99"}
	page, next, hasMore := PaginateSlice(items, cursor, 2)
	if len(page) != 0 || hasMore || next.ID != "" {
		t.Errorf("expected empty result, got %v, %v, %v", page, next, hasMore)
	}
}

package pagination

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// PreviousCursor (PaginateSlice)
// ---------------------------------------------------------------------------

func TestPaginateSlice_PreviousCursor_FirstPageHasNone(t *testing.T) {
	items := []dummyItem{{"a", "10"}, {"b", "10"}, {"c", "20"}}
	page := PaginateSlice(items, Cursor{}, 2)
	if page.PreviousCursor != "" {
		t.Errorf("expected empty PreviousCursor on first page, got %q", page.PreviousCursor)
	}
}

func TestPaginateSlice_PreviousCursor_SecondPagePointsToFirst(t *testing.T) {
	items := []dummyItem{
		{"a", "10"}, {"b", "10"}, {"c", "20"}, {"d", "30"},
	}
	page1 := PaginateSlice(items, Cursor{}, 2)
	next1, _ := Decode(page1.NextCursor)

	page2 := PaginateSlice(items, next1, 2)
	if page2.PreviousCursor != "" {
		t.Errorf("expected empty PreviousCursor pointing back at the first page, got %q", page2.PreviousCursor)
	}
}

func TestPaginateSlice_PreviousCursor_RoundTripsToPriorPage(t *testing.T) {
	// Same fixture as TestPaginateSlice_ContinuityAndDuplicates.
	items := []dummyItem{
		{"a", "10"},
		{"b", "10"},
		{"c", "20"},
		{"d", "30"},
		{"e", "30"},
		{"f", "40"},
		{"g", "50"},
	}
	const limit = 2

	page1 := PaginateSlice(items, Cursor{}, limit)
	next1, _ := Decode(page1.NextCursor)

	page2 := PaginateSlice(items, next1, limit)
	next2, _ := Decode(page2.NextCursor)

	page3 := PaginateSlice(items, next2, limit)
	if page3.PreviousCursor == "" {
		t.Fatal("expected non-empty PreviousCursor on the third page")
	}

	// Following page3's PreviousCursor must reproduce page2 exactly.
	prevCursor, err := Decode(page3.PreviousCursor)
	if err != nil {
		t.Fatalf("Decode(PreviousCursor): %v", err)
	}
	reconstructed := PaginateSlice(items, prevCursor, limit)
	if len(reconstructed.Items) != len(page2.Items) {
		t.Fatalf("reconstructed page has %d items, want %d", len(reconstructed.Items), len(page2.Items))
	}
	for i := range page2.Items {
		if reconstructed.Items[i].id != page2.Items[i].id {
			t.Errorf("item %d: got %s, want %s", i, reconstructed.Items[i].id, page2.Items[i].id)
		}
	}
}

func TestPaginateSlice_PreviousCursor_EmptyResult(t *testing.T) {
	page := PaginateSlice([]dummyItem{}, Cursor{}, 10)
	if page.PreviousCursor != "" {
		t.Errorf("expected empty PreviousCursor for empty result, got %q", page.PreviousCursor)
	}
}

// ---------------------------------------------------------------------------
// LinkHeader
// ---------------------------------------------------------------------------

func TestLinkHeader_FirstPage_OmitsPrev(t *testing.T) {
	header := LinkHeader("https://api.example.com/api/v1/plans?limit=10", LinkParams{
		Next:    "next-cursor",
		HasPrev: false,
	})

	if strings.Contains(header, `rel="prev"`) {
		t.Errorf("expected no prev link on first page, got %q", header)
	}
	if !strings.Contains(header, `rel="first"`) {
		t.Errorf("expected first link, got %q", header)
	}
	if !strings.Contains(header, `rel="next"`) {
		t.Errorf("expected next link, got %q", header)
	}
}

func TestLinkHeader_LastPage_OmitsNext(t *testing.T) {
	header := LinkHeader("https://api.example.com/api/v1/plans?limit=10", LinkParams{
		Next:    "",
		Prev:    "prev-cursor",
		HasPrev: true,
	})

	if strings.Contains(header, `rel="next"`) {
		t.Errorf("expected no next link on last/empty page, got %q", header)
	}
	if !strings.Contains(header, `rel="prev"`) {
		t.Errorf("expected prev link, got %q", header)
	}
	if !strings.Contains(header, `rel="first"`) {
		t.Errorf("expected first link, got %q", header)
	}
}

func TestLinkHeader_EmptyPage_OnlyFirst(t *testing.T) {
	header := LinkHeader("https://api.example.com/api/v1/plans?limit=10", LinkParams{})

	if strings.Contains(header, `rel="next"`) || strings.Contains(header, `rel="prev"`) {
		t.Errorf("expected only a first link for an empty first page, got %q", header)
	}
	if !strings.Contains(header, `rel="first"`) {
		t.Errorf("expected first link, got %q", header)
	}
}

func TestLinkHeader_PreservesExistingQueryParams(t *testing.T) {
	header := LinkHeader("https://api.example.com/api/v1/subscriptions?limit=25&status=active", LinkParams{
		Next: "abc123",
	})

	for _, want := range []string{"limit=25", "status=active", "cursor=abc123"} {
		if !strings.Contains(header, want) {
			t.Errorf("expected header to contain %q, got %q", want, header)
		}
	}
}

func TestLinkHeader_ReplacesExistingCursorParam(t *testing.T) {
	header := LinkHeader("https://api.example.com/api/v1/plans?cursor=stale&limit=10", LinkParams{
		Next:    "fresh-next",
		Prev:    "fresh-prev",
		HasPrev: true,
	})

	if strings.Contains(header, "stale") {
		t.Errorf("expected stale cursor to be replaced, got %q", header)
	}
	if !strings.Contains(header, "cursor=fresh-next") {
		t.Errorf("expected next link to carry fresh-next cursor, got %q", header)
	}
}

func TestLinkHeader_FirstLinkHasNoCursorParam(t *testing.T) {
	header := LinkHeader("https://api.example.com/api/v1/plans?cursor=xyz&limit=10", LinkParams{
		HasPrev: true,
		Prev:    "prev-cursor",
	})

	// Extract the <...> target for rel="first".
	start := strings.Index(header, "<")
	end := strings.Index(header, ">")
	if start < 0 || end < 0 {
		t.Fatalf("could not find first link target in %q", header)
	}
	firstTarget := header[start+1 : end]
	if strings.Contains(firstTarget, "cursor=") {
		t.Errorf("expected rel=first target to have no cursor param, got %q", firstTarget)
	}
}

func TestLinkHeader_RejectsRelativeBaseURL(t *testing.T) {
	header := LinkHeader("/api/v1/plans?limit=10", LinkParams{Next: "abc"})
	if header != "" {
		t.Errorf("expected empty header for relative baseURL, got %q", header)
	}
}

func TestLinkHeader_RejectsInvalidBaseURL(t *testing.T) {
	header := LinkHeader("://not a url", LinkParams{Next: "abc"})
	if header != "" {
		t.Errorf("expected empty header for invalid baseURL, got %q", header)
	}
}

func TestLinkHeader_MultipleLinksAreCommaSeparated(t *testing.T) {
	header := LinkHeader("https://api.example.com/api/v1/plans?limit=10", LinkParams{
		Next:    "next-cursor",
		Prev:    "prev-cursor",
		HasPrev: true,
	})

	parts := strings.Split(header, ", ")
	if len(parts) != 3 {
		t.Fatalf("expected 3 comma-separated links (first, prev, next), got %d: %q", len(parts), header)
	}
}

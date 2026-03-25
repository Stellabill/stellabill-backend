package pagination

import (
	"strconv"
	"testing"
)

type dummy struct {
	ID    int
	Price int
}

func getSortVal(d dummy, sortBy string) string {
	if sortBy == "price" {
		return strconv.Itoa(d.Price)
	}
	// Sort by ID is default fallback in tests where sortBy is blank or non-existent
	return strconv.Itoa(d.ID)
}

func TestPaginateList_SaneBounds(t *testing.T) {
	items := []dummy{{1, 10}, {2, 20}, {3, 30}}

	// Negative offset -> 0
	// Zero limit -> 10
	page, meta := PaginateList(items, -5, 0, "", "", getSortVal)
	if len(page) != 3 || meta.Total != 3 || meta.HasMore != false {
		t.Errorf("Bounds defaulting failed: %v", meta)
	}

	// Limit exceeds 100 -> cap to 100
	items105 := make([]dummy, 105)
	for i := 0; i < 105; i++ {
		items105[i] = dummy{ID: i, Price: i}
	}

	pageMax, metaMax := PaginateList(items105, 0, 999, "price", "asc", getSortVal)
	if len(pageMax) != 100 || metaMax.HasMore != true || metaMax.NextOffset != 100 {
		t.Errorf("Max limit cap failed: len %d, meta %v", len(pageMax), metaMax)
	}
}

func TestPaginateList_OffsetBeyondRangeAndEmpty(t *testing.T) {
	items := []dummy{{1, 10}, {2, 20}}

	// empty result set
	pageE, metaE := PaginateList([]dummy{}, 0, 10, "price", "asc", getSortVal)
	if len(pageE) != 0 || metaE.Total != 0 || metaE.HasMore {
		t.Error("Empty set failed")
	}

	// Offset beyond range
	pageO, metaO := PaginateList(items, 10, 5, "price", "desc", getSortVal)
	if len(pageO) != 0 || metaO.Total != 2 || metaO.HasMore {
		t.Error("Offset beyond range failed")
	}
}

func TestPaginateList_Sorting(t *testing.T) {
	items := []dummy{
		{ID: 1, Price: 300},
		{ID: 2, Price: 100},
		{ID: 3, Price: 200},
	}

	// Sort Ascending
	page, _ := PaginateList(items, 0, 10, "price", "asc", getSortVal)
	if page[0].Price != 100 || page[1].Price != 200 || page[2].Price != 300 {
		t.Errorf("Asc sorting failed: %v", page)
	}

	// Sort Descending
	pageD, _ := PaginateList(items, 0, 10, "price", "desc", getSortVal)
	if pageD[0].Price != 300 || pageD[1].Price != 200 || pageD[2].Price != 100 {
		t.Errorf("Desc sorting failed: %v", pageD)
	}
}

func TestPaginateList_PaginationLogic(t *testing.T) {
	items := []dummy{{1, 10}, {2, 20}, {3, 30}, {4, 40}, {5, 50}}

	// Page 1
	page1, meta1 := PaginateList(items, 0, 2, "id", "asc", getSortVal)
	if len(page1) != 2 || meta1.HasMore != true || meta1.NextOffset != 2 {
		t.Errorf("Page1 mismatch: %+v", meta1)
	}

	// Page 2
	page2, meta2 := PaginateList(items, meta1.NextOffset, 2, "id", "asc", getSortVal)
	if len(page2) != 2 || meta2.HasMore != true || meta2.NextOffset != 4 || page2[0].ID != 3 {
		t.Errorf("Page2 mismatch: %+v", meta2)
	}

	// Page 3
	page3, meta3 := PaginateList(items, meta2.NextOffset, 2, "id", "asc", getSortVal)
	if len(page3) != 1 || meta3.HasMore != false || meta3.NextOffset != 0 || page3[0].ID != 5 {
		t.Errorf("Page3 mismatch: %+v", meta3)
	}
}

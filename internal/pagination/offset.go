package pagination

import (
	"cmp"
	"slices"
	"strings"
)

// ListMetadata contains pagination metadata for offset queries.
type ListMetadata struct {
	Total      int  `json:"total"`
	NextOffset int  `json:"next_offset,omitempty"`
	HasMore    bool `json:"has_more"`
}

// PaginateList handles the logic for slicing arrays to simulate SQL offset/limit/sort.
// T must support field extraction for sorting.
// It returns the sliced items and corresponding metadata.
func PaginateList[T any](items []T, offset, limit int, sortBy, sortOrder string, getSortVal func(T, string) string) ([]T, ListMetadata) {
	// 1. Sane bounds enforcement
	if limit <= 0 {
		limit = 10
	} else if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	// 2. Sorting mechanism (simulating ORDER BY)
	if sortBy != "" {
		slices.SortStableFunc(items, func(a, b T) int {
			valA, valB := getSortVal(a, sortBy), getSortVal(b, sortBy)
			cmpRes := cmp.Compare(valA, valB)
			if strings.EqualFold(sortOrder, "desc") {
				return -cmpRes
			}
			return cmpRes
		})
	}

	// 3. Evaluate Boundaries
	total := len(items)

	// If the offset strictly exceeds the available data points, or there are no items
	if total == 0 || offset >= total {
		return []T{}, ListMetadata{Total: total, NextOffset: 0, HasMore: false}
	}

	// Calculate slice stop
	endIdx := offset + limit
	hasMore := true
	if endIdx >= total {
		endIdx = total
		hasMore = false
	}

	page := items[offset:endIdx]

	nextOff := 0
	if hasMore {
		nextOff = endIdx
	}

	return page, ListMetadata{
		Total:      total,
		NextOffset: nextOff,
		HasMore:    hasMore,
	}
}

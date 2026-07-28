package pagination

import (
	"net/url"
	"strings"
)

// CursorParam is the query parameter name used to carry the opaque cursor.
const CursorParam = "cursor"

// LinkParams describes the cursor state needed to build an RFC 5988 (RFC
// 8288) Link header for a cursor-paginated list endpoint.
type LinkParams struct {
	// Next is the opaque cursor for the next page (Page.NextCursor). Empty
	// means there is no next page, and rel="next" is omitted.
	Next string
	// Prev is the opaque cursor that reconstructs the previous page
	// (Page.PreviousCursor). An empty Prev still produces a valid rel="prev"
	// link (pointing at the first page, i.e. with no cursor parameter) as
	// long as HasPrev is true.
	Prev string
	// HasPrev must be true only when the current request is not for the
	// first page (i.e. the incoming request supplied a non-empty cursor).
	// This is what distinguishes "no previous page" from "the previous page
	// has no cursor of its own".
	HasPrev bool
}

// LinkHeader builds an RFC 5988 Link header value for a cursor-paginated list
// endpoint:
//
//   - rel="first" is always included (a stable link back to the unfiltered
//     first page's cursor position).
//   - rel="prev" is included only when params.HasPrev is true.
//   - rel="next" is included only when params.Next is non-empty.
//
// baseURL must be an absolute URL (scheme + host, e.g. as reconstructed from
// the incoming request) and should already include any filter/limit query
// parameters that must be preserved across pages; LinkHeader only adds or
// replaces the cursor parameter, it never invents or drops other query
// parameters. Returns "" if baseURL is not a valid absolute URL, so a
// malformed base can never produce a malformed or misleading Link header.
func LinkHeader(baseURL string, params LinkParams) string {
	u, err := url.Parse(baseURL)
	if err != nil || !u.IsAbs() {
		return ""
	}

	var b strings.Builder
	writeLink := func(cursor, rel string) {
		if b.Len() > 0 {
			b.WriteString(", ")
		}
		b.WriteString("<")
		b.WriteString(withCursor(u, cursor))
		b.WriteString(`>; rel="`)
		b.WriteString(rel)
		b.WriteString(`"`)
	}

	writeLink("", "first")
	if params.HasPrev {
		writeLink(params.Prev, "prev")
	}
	if params.Next != "" {
		writeLink(params.Next, "next")
	}

	return b.String()
}

// withCursor returns an absolute URL string equal to u with the cursor query
// parameter set to cursor (or removed, if cursor is empty). u itself is not
// mutated.
func withCursor(u *url.URL, cursor string) string {
	v := *u
	q := u.Query()
	if cursor == "" {
		q.Del(CursorParam)
	} else {
		q.Set(CursorParam, cursor)
	}
	v.RawQuery = q.Encode()
	return v.String()
}

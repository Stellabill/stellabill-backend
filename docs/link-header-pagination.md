# RFC 5988 Link header pagination

Cursor-paginated list endpoints emit a standard `Link` response header
(RFC 5988 / RFC 8288) alongside the existing JSON cursor fields
(`next_cursor`, `has_more`), so generic HTTP clients can walk a collection
without parsing the response body.

```
Link: <https://api.example.com/api/v1/plans?limit=20>; rel="first",
      <https://api.example.com/api/v1/plans?limit=20&cursor=xyz>; rel="prev",
      <https://api.example.com/api/v1/plans?limit=20&cursor=abc>; rel="next"
```

- `rel="first"` is always present: a stable link back to the first page.
- `rel="prev"` is present only when the request was not itself for the first
  page (i.e. it supplied a non-empty `cursor`).
- `rel="next"` is present only when more results exist.
- Link targets are absolute and preserve every other query parameter from the
  request (filters, `limit`, etc.) — only the `cursor` parameter is
  added/replaced.

## Implementation

- `internal/pagination/link_header.go`: `LinkHeader(baseURL string, params LinkParams) string`
  is the framework-agnostic helper that builds the header value. It returns
  `""` (no header) if `baseURL` isn't a valid absolute URL, so a bad base can
  never produce a malformed or misleading `Link` header.
- `internal/pagination/cursor.go`: `PaginateSlice` now also returns
  `Page.PreviousCursor`, computed from the same in-memory slice used for
  forward pagination — no new backward-pagination mechanism was introduced.
- `internal/handlers/pagination_links.go`: `requestBaseURL` reconstructs the
  absolute request URL from the Gin context (nil-safe: handlers are sometimes
  invoked in tests with `c.Request == nil`, in which case no header is set).
  `setPaginationLinkHeader` wires a `pagination.Page` into `LinkHeader` and
  sets the response header.

## Wired endpoints

| Endpoint | Links |
|---|---|
| `GET /api/v1/plans`, `GET /api/plans` | first, prev, next |
| `GET /api/subscriptions` | first, prev, next |
| `GET /api/v1/statements` | first only (see below) |

## Statements: first-only

`GET /api/v1/statements` does not get `rel="next"`/`rel="prev"` links. Its
repository layer (`repository.StatementQuery.StartingAfter`/`EndingBefore`)
defines cursor fields but no repository implementation reads them, and the
handler doesn't accept them as query parameters — there is no working
keyset-pagination mechanism to link to today. Emitting `next`/`prev` links
here would point at a URL that returns the same first page again. Wiring
real cursor pagination through to the statements repository is a separate,
larger change.

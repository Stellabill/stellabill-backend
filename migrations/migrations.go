// Package migrations exposes the embedded SQL migration files for use by
// the testutil package and any future migration runner.
package migrations

import "embed"

// FS contains all *.sql files in the migrations directory, embedded at build time.
//
//go:embed *.sql
var FS embed.FS


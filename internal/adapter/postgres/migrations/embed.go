// Package migrations holds versioned SQL applied at startup.
package migrations

import "embed"

// FS contains goose migration files.
//
//go:embed *.sql
var FS embed.FS

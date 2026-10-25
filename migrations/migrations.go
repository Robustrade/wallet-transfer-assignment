// Package migrations embeds the SQL schema migrations.
package migrations

import "embed"

// FS holds the ordered *.sql migration files.
//
//go:embed *.sql
var FS embed.FS

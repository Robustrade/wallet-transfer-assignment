// Package migrations embeds the SQL schema files and applies them in order.
package migrations

import "embed"

// FS holds the migration files, applied in filename order.
//
//go:embed *.sql
var FS embed.FS

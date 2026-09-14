// Package migrations embeds the SQL schema migrations so a binary carries its
// own schema and needs no files on disk at runtime.
package migrations

import "embed"

// FS holds every .sql migration, applied in lexical filename order.
//
//go:embed *.sql
var FS embed.FS

// Package migrations embeds the SQL schema migrations so they can be applied by
// the server on startup without an external migrate binary.
package migrations

import "embed"

// FS holds the versioned migration files (NNNN_name.up.sql / .down.sql).
//
//go:embed *.sql
var FS embed.FS

// Package migrations embeds the SQL schema migrations into the binary.
package migrations

import "embed"

// Files holds every *.sql migration in this directory.
//
//go:embed *.sql
var Files embed.FS

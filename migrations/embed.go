// Package migrations embeds the versioned SQL schema migrations and demo seed data,
// so the binary is self-sufficient and the schema ships with the code that needs it.
package migrations

import "embed"

// FS holds schema migrations (NNNN_name.sql, applied in lexical order) and
// seed data (seed/*.sql, idempotent, applied only on request).
//
//go:embed *.sql seed/*.sql
var FS embed.FS

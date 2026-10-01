// Package migrations embeds the SQL migration files so the server binary can
// apply them at startup.
package migrations

import "embed"

// FS holds every *.sql file in this directory (both .up.sql and .down.sql).
//
//go:embed *.sql
var FS embed.FS

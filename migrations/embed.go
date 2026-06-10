// Package migrations embeds all SQL migration files so that the binary is
// self-contained and does not require the migrations directory at runtime.
package migrations

import "embed"

// FS is an embedded file system containing all migration SQL files.
// Use it with db.Migrate to apply migrations from any environment without
// needing the migration files on disk at runtime.
//
//go:embed *.sql
var FS embed.FS

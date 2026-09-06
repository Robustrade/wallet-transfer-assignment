// Package migrations embeds the raw SQL migration files so the binary can
// apply schema on startup without shelling out to a separate migrate tool.
package migrations

import _ "embed"

//go:embed 0001_init.sql
var InitSQL string

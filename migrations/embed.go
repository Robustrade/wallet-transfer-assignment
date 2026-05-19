// sql files get baked into the binary via go:embed so we don't depend
// on the filesystem layout when deployed.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS

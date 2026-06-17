// Package migrate applies the database schema and seed data. The SQL files are
// embedded so the same migrations are used by the server on startup and by the
// test harness. Files in sql/ are applied in lexical order (0001_, 0002_, ...).
package migrate

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed sql/*.sql
var migrationsFS embed.FS

// Apply runs every embedded .sql migration in order. Each file is written
// idempotently, so Apply is safe to run on every startup.
func Apply(ctx context.Context, pool *pgxpool.Pool) error {
	entries, err := migrationsFS.ReadDir("sql")
	if err != nil {
		return err
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, n := range names {
		b, err := migrationsFS.ReadFile("sql/" + n)
		if err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, string(b)); err != nil {
			return fmt.Errorf("apply %s: %w", n, err)
		}
	}
	return nil
}

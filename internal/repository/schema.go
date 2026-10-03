package repository

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	//go:embed sql/schema.sql
	schemaSQL string

	//go:embed sql/seed.sql
	seedSQL string
)

// ApplySchema creates the types and tables that don't exist yet, so it is
// safe to run on every startup.
func ApplySchema(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, schemaSQL); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}

// ApplySeed inserts the seed wallets that don't exist yet, so it is safe to
// run on every startup.
func ApplySeed(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, seedSQL); err != nil {
		return fmt.Errorf("apply seed: %w", err)
	}
	return nil
}

package migrations

import (
	"context"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed *.sql
var FS embed.FS

func initGoose() error {
	goose.SetBaseFS(FS)
	return goose.SetDialect("postgres")
}

// Up applies all pending database migrations.
func Up(ctx context.Context, pool *pgxpool.Pool) error {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	if err := initGoose(); err != nil {
		return fmt.Errorf("failed to initialize goose: %w", err)
	}
	return goose.UpContext(ctx, db, ".")
}

// Down rolls back the last database migration.
func Down(ctx context.Context, pool *pgxpool.Pool) error {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	if err := initGoose(); err != nil {
		return fmt.Errorf("failed to initialize goose: %w", err)
	}
	return goose.DownContext(ctx, db, ".")
}

// Status prints the status of all migrations.
func Status(ctx context.Context, pool *pgxpool.Pool) error {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	if err := initGoose(); err != nil {
		return fmt.Errorf("failed to initialize goose: %w", err)
	}
	return goose.StatusContext(ctx, db, ".")
}

// Run is a convenience alias for Up.
func Run(ctx context.Context, pool *pgxpool.Pool) error {
	return Up(ctx, pool)
}

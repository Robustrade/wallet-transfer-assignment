package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jaiminbhaduri/wallet-transfer-assignment/migrations"
)

// DBTX is satisfied by both *pgxpool.Pool and pgx.Tx, letting repositories
// run against either a pooled connection or an in-flight transaction.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return pool, nil
}

// Migrate applies the embedded schema. Statements are idempotent
// (CREATE TABLE IF NOT EXISTS / ON CONFLICT DO NOTHING) so it's safe to run
// on every startup.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	for _, stmt := range splitStatements(migrations.InitSQL) {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("migrate: exec statement failed: %w\nstatement: %s", err, stmt)
		}
	}
	return nil
}

func splitStatements(sql string) []string {
	var stmts []string
	for _, raw := range strings.Split(sql, ";") {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		stmts = append(stmts, s)
	}
	return stmts
}

package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Robustrade/wallet-transfer-assignment/internal/database"
)

type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type queries struct {
	q  querier
	db *database.DB
}

func (c *queries) exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return c.q.ExecContext(ctx, c.db.Rebind(query), args...)
}

func (c *queries) queryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return c.q.QueryRowContext(ctx, c.db.Rebind(query), args...)
}

func (c *queries) query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return c.q.QueryContext(ctx, c.db.Rebind(query), args...)
}

type Store struct {
	*queries
}

func NewStore(db *database.DB) *Store {
	return &Store{queries: &queries{q: db, db: db}}
}

type Tx struct {
	*queries
}

func (s *Store) WithTx(ctx context.Context, fn func(*Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := fn(&Tx{queries: &queries{q: tx, db: s.db}}); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	committed = true
	return nil
}

func noRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }

package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func (r *Repository) WithTransaction(
	ctx context.Context,
	fn func(tx pgx.Tx) error,
) error {

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

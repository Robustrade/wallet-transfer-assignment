package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func (r *Repository) WithTx(ctx context.Context, txFn func(ctx context.Context, tx pgx.Tx) error) error {

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}

	defer tx.Rollback(ctx)

	if err := txFn(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

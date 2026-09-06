package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"wallet-transfer-assignment/internal/repository"
)

type UnitOfWork struct {
	db *sql.DB
}

func NewUnitOfWork(db *sql.DB) *UnitOfWork {
	return &UnitOfWork{db: db}
}

func (u *UnitOfWork) RunWriteTx(ctx context.Context, fn func(ctx context.Context, repos repository.Repos) error) error {
	tx, err := u.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	repos := repository.Repos{
		Wallets:   &walletRepo{tx: tx},
		Transfers: &transferRepo{tx: tx},
		Ledger:    &ledgerRepo{tx: tx},
	}

	if err := fn(ctx, repos); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("run tx: %w (rollback also failed: %v)", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (u *UnitOfWork) RunReadTx(ctx context.Context, fn func(ctx context.Context, repos repository.Repos) error) error {
	tx, err := u.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin read tx: %w", err)
	}
	defer tx.Rollback()

	repos := repository.Repos{
		Wallets:   &walletRepo{tx: tx},
		Transfers: &transferRepo{tx: tx},
		Ledger:    &ledgerRepo{tx: tx},
	}
	return fn(ctx, repos)
}

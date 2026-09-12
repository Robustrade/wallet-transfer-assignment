package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-transfer-assignment/internal/repository"
)

type txManager struct {
	pool *pgxpool.Pool
}

// NewTxManager constructs a repository.TxManager backed by a PostgreSQL connection pool.
func NewTxManager(pool *pgxpool.Pool) repository.TxManager {
	return &txManager{pool: pool}
}

func (m *txManager) ExecuteInTx(ctx context.Context, fn func(txRepos repository.Repositories) error) error {
	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.ReadCommitted,
	})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		// Rollback is a safe no-op if tx.Commit has already succeeded.
		// Use an independent context with timeout so rollback completes even if the caller's context was canceled.
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()

	repos := repository.Repositories{
		Wallets:     NewWalletRepository(tx),
		Transfers:   NewTransferRepository(tx),
		Ledger:      NewLedgerRepository(tx),
		Idempotency: NewIdempotencyRepository(tx),
	}

	if err := fn(repos); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

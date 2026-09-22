// Package postgres implements the repository interfaces on top of
// PostgreSQL using pgx/v5. It is the production persistence backend.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wallet-transfer-assignment/wallet-transfer/internal/repository"
)

// dbtx is the subset of the pgx API shared by *pgxpool.Pool and pgx.Tx. Each
// repository is written against dbtx so the same code runs whether it is
// executing inside a transaction (the common case, via UnitOfWork) or
// directly against the pool for simple read-only queries.
type dbtx interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// UnitOfWork implements repository.UnitOfWork on top of a pgx connection pool.
type UnitOfWork struct {
	pool             *pgxpool.Pool
	maxClaimAttempts int
}

// NewUnitOfWork wraps an existing pool.
func NewUnitOfWork(pool *pgxpool.Pool, maxClaimAttempts int) *UnitOfWork {
	return &UnitOfWork{pool: pool, maxClaimAttempts: maxClaimAttempts}
}

// Execute runs fn inside a single read-committed transaction with explicit
// row locking performed by the repositories themselves (see
// WalletRepository.GetForUpdate). The transaction is rolled back
// automatically if fn returns an error or panics.
func (u *UnitOfWork) Execute(ctx context.Context, fn func(ctx context.Context, repos repository.Repositories) error) error {
	tx, err := u.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		// Rollback is a no-op if the transaction was already committed.
		_ = tx.Rollback(ctx)
	}()

	repos := repository.Repositories{
		Wallets:     &walletRepo{db: tx},
		Transfers:   &transferRepo{db: tx},
		Ledger:      &ledgerRepo{db: tx},
		Idempotency: &idempotencyRepo{db: tx, maxClaimAttempts: u.maxClaimAttempts},
	}

	if err := fn(ctx, repos); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// isUniqueViolation reports whether err is a Postgres unique_violation
// (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

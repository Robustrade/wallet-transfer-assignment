package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/robustrade/wallet-transfer-assignment/internal/domain"
)

// querier is the subset of pgx.Tx / pgxpool.Pool that repositories need.
// Every repository method accepts a ctx and resolves its querier from it,
// so the same repository code works whether or not it's inside a transaction.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type txCtxKey struct{}

func withTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, txCtxKey{}, tx)
}

// TxManager implements repository.TxManager on top of a pgx pool.
type TxManager struct {
	pool *pgxpool.Pool
}

func NewTxManager(pool *pgxpool.Pool) *TxManager {
	return &TxManager{pool: pool}
}

func (m *TxManager) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	txCtx := withTx(ctx, tx)

	if err := fn(txCtx); err != nil {
		// Deliberately not using ctx here: if the caller's context is what
		// caused fn to fail (cancelled/timed out), it may already be done,
		// and Rollback(ctx) would fail immediately without ever telling
		// Postgres to roll back -- leaving the transaction open on this
		// connection until the pool eventually notices. Cleanup must still
		// run even when the original context is dead, so it gets its own
		// short-lived, always-valid context instead.
		rbCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if rbErr := tx.Rollback(rbCtx); rbErr != nil && rbErr != pgx.ErrTxClosed {
			return fmt.Errorf("%w (rollback also failed: %v)", err, rbErr)
		}
		return err
	}

	// Deliberately not using ctx here either: fn already succeeded, so this
	// commit represents work that should be finalized regardless of whether
	// the caller's context is subsequently cancelled/times out. Using ctx
	// would let an unrelated cancellation abort the commit and surface as an
	// ambiguous failure (see ErrCommitOutcomeUnknown below) far more often
	// than genuine network/DB errors would.
	commitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := tx.Commit(commitCtx); err != nil {
		// The COMMIT may have reached and been applied by the server even
		// though this client-side call errored (e.g. the ack was lost) --
		// unlike Rollback, there is no safe way to tell "definitely not
		// applied" apart from "applied, but we didn't hear back" here.
		// Wrap with ErrCommitOutcomeUnknown so callers (idempotency release
		// logic in particular) know not to treat this as a no-op.
		return fmt.Errorf("commit tx: %w: %w", domain.ErrCommitOutcomeUnknown, err)
	}
	return nil
}

// db resolves the active transaction from ctx if WithinTx put one there,
// otherwise falls back to the pool directly (for reads outside a transaction).
type db struct {
	pool *pgxpool.Pool
}

func (d *db) q(ctx context.Context) querier {
	if tx, ok := ctx.Value(txCtxKey{}).(pgx.Tx); ok {
		return tx
	}
	return d.pool
}

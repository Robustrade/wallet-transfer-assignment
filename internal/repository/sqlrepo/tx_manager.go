package sqlrepo

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/LokilokeshM/wallet-transfer-assignment/internal/service"
)

// DefaultMaxAttempts is how many times WithinTx runs a transaction before
// giving up on retryable conflicts.
const DefaultMaxAttempts = 3

// TxManager implements service.TxManager on a *sql.DB.
type TxManager struct {
	db          *sql.DB
	d           Dialect
	maxAttempts int
	backoff     func(attempt int) time.Duration
}

// TxOption customizes a TxManager.
type TxOption func(*TxManager)

// WithBackoff replaces the delay between attempts. attempt is the number of
// the attempt that just failed (1, 2, ...). Tests use it to remove the delay
// and to count retries.
func WithBackoff(f func(attempt int) time.Duration) TxOption {
	return func(m *TxManager) { m.backoff = f }
}

// NewTxManager returns a TxManager that retries up to DefaultMaxAttempts
// times with jittered exponential backoff.
func NewTxManager(db *sql.DB, d Dialect, opts ...TxOption) *TxManager {
	m := &TxManager{db: db, d: d, maxAttempts: DefaultMaxAttempts, backoff: jitteredBackoff}
	for _, o := range opts {
		o(m)
	}
	return m
}

// jitteredBackoff waits a random duration in [0, 10ms * 2^(attempt-1)),
// using "full jitter". The randomness spreads out transactions that just
// deadlocked with each other, so they do not collide again in lockstep.
func jitteredBackoff(attempt int) time.Duration {
	ceiling := 10 * time.Millisecond << (attempt - 1)
	return rand.N(ceiling)
}

// WithinTx implements service.TxManager.
//
// The whole transaction (BEGIN, fn, COMMIT) is retried, never a single
// statement. This is safe because every effect of fn, including the
// idempotency record insert, is inside the transaction being retried. A
// failed attempt leaves nothing behind.
func (m *TxManager) WithinTx(ctx context.Context, fn func(service.Repos) error) error {
	for attempt := 1; ; attempt++ {
		err := m.runOnce(ctx, fn)
		if err == nil {
			return nil
		}

		// Classify the failure. The order matters: a cancelled context can
		// surface as many different driver errors, so check it first.
		switch {
		case ctx.Err() != nil:
			return fmt.Errorf("%w: %w", service.ErrUnavailable, ctx.Err())
		case m.d.IsLockTimeout(err):
			return fmt.Errorf("%w: lock wait timed out: %w", service.ErrUnavailable, err)
		case !m.d.IsRetryable(err):
			return err // business errors (domain.Err*) and unexpected failures, unchanged
		case attempt >= m.maxAttempts:
			return fmt.Errorf("%w: gave up after %d attempts: %w", service.ErrUnavailable, attempt, err)
		}

		// Wait before retrying, but stop early if the request is cancelled.
		timer := time.NewTimer(m.backoff(attempt))
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("%w: %w", service.ErrUnavailable, ctx.Err())
		}
	}
}

// runOnce executes a single attempt: begin, set up, run fn, commit.
// Any error, or a panic in fn, rolls the transaction back.
func (m *TxManager) runOnce(ctx context.Context, fn func(service.Repos) error) (err error) {
	tx, err := m.db.BeginTx(ctx, m.d.TxOptions())
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	committed := false
	defer func() {
		// Also runs when fn panics or calls runtime.Goexit (t.FailNow in tests),
		// so the connection never goes back to the pool mid-transaction.
		if !committed {
			_ = tx.Rollback() // the original error matters more than a rollback error
		}
	}()

	if err := m.d.SetupTx(ctx, tx); err != nil {
		return fmt.Errorf("set up transaction: %w", err)
	}
	if err := fn(NewRepos(tx, m.d)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	committed = true
	return nil
}

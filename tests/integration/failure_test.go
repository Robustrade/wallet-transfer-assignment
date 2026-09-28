package integration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LokilokeshM/wallet-transfer-assignment/internal/config"
	"github.com/LokilokeshM/wallet-transfer-assignment/internal/domain"
	"github.com/LokilokeshM/wallet-transfer-assignment/internal/repository/sqlrepo"
	"github.com/LokilokeshM/wallet-transfer-assignment/internal/service"
)

// errFakeRetryable stands in for a deadlock or serialization failure, which
// cannot be produced on demand.
var errFakeRetryable = errors.New("fake retryable conflict")

// fakeRetryDialect is the real dialect, except that it also treats
// errFakeRetryable as retryable.
type fakeRetryDialect struct{ sqlrepo.Dialect }

func (d fakeRetryDialect) IsRetryable(err error) bool {
	return errors.Is(err, errFakeRetryable) || d.Dialect.IsRetryable(err)
}

// txHarness builds a TxManager over env's database that counts attempts
// (calls to fn) and retries (backoff sleeps between attempts).
type txHarness struct {
	tm       *sqlrepo.TxManager
	retries  int
	sleepFor time.Duration
	onSleep  func()
}

func newTxHarness(e *testEnv) *txHarness {
	h := &txHarness{}
	h.tm = sqlrepo.NewTxManager(e.db, fakeRetryDialect{e.dialect},
		sqlrepo.WithBackoff(func(attempt int) time.Duration {
			h.retries++
			if h.onSleep != nil {
				h.onSleep()
			}
			return h.sleepFor
		}))
	return h
}

// writeTransfer performs a complete, valid transfer through the repositories.
// It gives each attempt observable writes, so the tests can prove what was
// committed and what was rolled back.
func writeTransfer(ctx context.Context, r service.Repos, key, from, to string, amount int64) error {
	tr, err := domain.NewTransfer(key, from, to, amount, time.Now())
	if err != nil {
		return err
	}
	if _, err := r.Idempotency.Insert(ctx, key, strings.Repeat("0", 64), tr.CreatedAt); err != nil {
		return err
	}
	fromW, err := r.Wallets.GetForUpdate(ctx, from)
	if err != nil {
		return err
	}
	toW, err := r.Wallets.GetForUpdate(ctx, to)
	if err != nil {
		return err
	}
	if err := r.Transfers.Create(ctx, tr); err != nil {
		return err
	}
	fromBefore, toBefore := fromW.Balance, toW.Balance
	if err := domain.ApplyTransfer(fromW, toW, amount); err != nil {
		return err
	}
	if err := r.Wallets.UpdateBalance(ctx, from, fromBefore, fromW.Balance, tr.CreatedAt); err != nil {
		return err
	}
	if err := r.Wallets.UpdateBalance(ctx, to, toBefore, toW.Balance, tr.CreatedAt); err != nil {
		return err
	}
	debit, credit := domain.NewLedgerEntries(tr)
	if err := r.Ledger.Insert(ctx, debit, credit); err != nil {
		return err
	}
	if err := tr.MarkProcessed(); err != nil {
		return err
	}
	if err := r.Transfers.UpdateStatus(ctx, tr, tr.CreatedAt); err != nil {
		return err
	}
	return r.Idempotency.LinkTransfer(ctx, key, tr.ID)
}

func TestTxManager(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T, driver string, opts ...envOption) (*testEnv, *txHarness) {
		e := newEnv(t, driver, opts...)
		e.seedWallet("a", 100, "SGD")
		e.seedWallet("b", 0, "SGD")
		return e, newTxHarness(e)
	}

	t.Run("commits on success and calls fn once", func(t *testing.T) {
		t.Parallel()
		forEachDriver(t, func(t *testing.T, driver string) {
			e, h := setup(t, driver)
			calls := 0

			err := h.tm.WithinTx(context.Background(), func(r service.Repos) error {
				calls++
				return writeTransfer(context.Background(), r, "k1", "a", "b", 10)
			})

			require.NoError(t, err)
			assert.Equal(t, 1, calls)
			assert.Equal(t, int64(1), e.count("transfers"))
			assert.Equal(t, int64(90), e.balance("a"))
		})
	})

	t.Run("rolls back everything and returns fn's error unchanged", func(t *testing.T) {
		t.Parallel()
		forEachDriver(t, func(t *testing.T, driver string) {
			e, h := setup(t, driver)
			boom := errors.New("boom")

			err := h.tm.WithinTx(context.Background(), func(r service.Repos) error {
				require.NoError(t, writeTransfer(context.Background(), r, "k1", "a", "b", 10))
				return boom
			})

			require.ErrorIs(t, err, boom)
			assert.NotErrorIs(t, err, service.ErrUnavailable)
			assert.Equal(t, 0, h.retries, "a non-retryable error must not be retried")
			assert.Equal(t, int64(0), e.count("transfers"))
			assert.Equal(t, int64(0), e.count("idempotency_records"))
			assert.Equal(t, int64(100), e.balance("a"))
		})
	})

	t.Run("domain errors pass through unwrapped and are not retried", func(t *testing.T) {
		t.Parallel()
		forEachDriver(t, func(t *testing.T, driver string) {
			_, h := setup(t, driver)

			err := h.tm.WithinTx(context.Background(), func(r service.Repos) error {
				_, err := r.Wallets.GetForUpdate(context.Background(), "ghost")
				return err
			})

			require.ErrorIs(t, err, domain.ErrWalletNotFound)
			assert.Equal(t, 0, h.retries)
		})
	})

	t.Run("retries the whole transaction: two conflicts then success commits exactly once", func(t *testing.T) {
		t.Parallel()
		forEachDriver(t, func(t *testing.T, driver string) {
			e, h := setup(t, driver)
			calls := 0

			err := h.tm.WithinTx(context.Background(), func(r service.Repos) error {
				calls++
				// Every attempt writes a full transfer. The failed attempts
				// must leave no trace, including their idempotency rows,
				// which would otherwise block the same key on the next try.
				if err := writeTransfer(context.Background(), r, "k1", "a", "b", 10); err != nil {
					return err
				}
				if calls < 3 {
					return errFakeRetryable
				}
				return nil
			})

			require.NoError(t, err)
			assert.Equal(t, 3, calls)
			assert.Equal(t, 2, h.retries)
			assert.Equal(t, int64(1), e.count("transfers"))
			assert.Equal(t, int64(2), e.count("ledger_entries"))
			assert.Equal(t, int64(90), e.balance("a"), "debited once, not three times")
		})
	})

	t.Run("gives up after 3 attempts with ErrUnavailable", func(t *testing.T) {
		t.Parallel()
		forEachDriver(t, func(t *testing.T, driver string) {
			e, h := setup(t, driver)
			calls := 0

			err := h.tm.WithinTx(context.Background(), func(r service.Repos) error {
				calls++
				require.NoError(t, writeTransfer(context.Background(), r, "k1", "a", "b", 10))
				return errFakeRetryable
			})

			require.ErrorIs(t, err, service.ErrUnavailable)
			require.ErrorIs(t, err, errFakeRetryable, "the cause is kept for logging")
			assert.Equal(t, 3, calls)
			assert.Equal(t, 2, h.retries)
			assert.Equal(t, int64(0), e.count("transfers"))
			assert.Equal(t, int64(100), e.balance("a"))
		})
	})

	t.Run("context cancelled before commit: nothing persisted, ErrUnavailable", func(t *testing.T) {
		t.Parallel()
		forEachDriver(t, func(t *testing.T, driver string) {
			e, h := setup(t, driver)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			err := h.tm.WithinTx(ctx, func(r service.Repos) error {
				require.NoError(t, writeTransfer(ctx, r, "k1", "a", "b", 10))
				cancel() // e.g. the client disconnected just before commit
				return nil
			})

			require.ErrorIs(t, err, service.ErrUnavailable)
			require.ErrorIs(t, err, context.Canceled)
			assert.Equal(t, 0, h.retries)
			assert.Equal(t, int64(0), e.count("transfers"))
			assert.Equal(t, int64(0), e.count("idempotency_records"))
			assert.Equal(t, int64(100), e.balance("a"))
		})
	})

	t.Run("already-cancelled context: fn is never called", func(t *testing.T) {
		t.Parallel()
		forEachDriver(t, func(t *testing.T, driver string) {
			_, h := setup(t, driver)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			calls := 0

			err := h.tm.WithinTx(ctx, func(service.Repos) error { calls++; return nil })

			require.ErrorIs(t, err, service.ErrUnavailable)
			assert.Equal(t, 0, calls)
		})
	})

	t.Run("context cancelled during backoff stops retrying", func(t *testing.T) {
		t.Parallel()
		forEachDriver(t, func(t *testing.T, driver string) {
			_, h := setup(t, driver)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			h.sleepFor = time.Hour // the test would hang if the sleep ignored ctx
			h.onSleep = cancel
			calls := 0

			err := h.tm.WithinTx(ctx, func(service.Repos) error { calls++; return errFakeRetryable })

			require.ErrorIs(t, err, service.ErrUnavailable)
			require.ErrorIs(t, err, context.Canceled)
			assert.Equal(t, 1, calls)
		})
	})

	t.Run("lock timeout returns ErrUnavailable without retrying", func(t *testing.T) {
		t.Parallel()
		forEachDriver(t, func(t *testing.T, driver string) {
			e, h := setup(t, driver, withLockTimeout(200*time.Millisecond))
			ctx := context.Background()

			// Another transaction holds the lock we need.
			holder, err := e.db.Conn(ctx)
			require.NoError(t, err)
			defer holder.Close()
			switch driver {
			case config.DriverSQLite:
				_, err = holder.ExecContext(ctx, "BEGIN IMMEDIATE") // takes the database write lock
			case config.DriverPostgres:
				_, err = holder.ExecContext(ctx, "BEGIN")
				if err == nil {
					_, err = holder.ExecContext(ctx, "SELECT id FROM wallets WHERE id = 'a' FOR UPDATE")
				}
			}
			require.NoError(t, err)
			defer func() { _, _ = holder.ExecContext(ctx, "ROLLBACK") }()

			start := time.Now()
			err = h.tm.WithinTx(ctx, func(r service.Repos) error {
				_, err := r.Wallets.GetForUpdate(ctx, "a")
				return err
			})

			require.ErrorIs(t, err, service.ErrUnavailable)
			assert.True(t, e.dialect.IsLockTimeout(err), "cause should be a lock timeout, got %v", err)
			assert.Equal(t, 0, h.retries, "a lock timeout already waited LOCK_TIMEOUT; retrying would exceed the request deadline")
			assert.GreaterOrEqual(t, time.Since(start), 150*time.Millisecond, "should have waited for the lock")
		})
	})
}

// cancelBeforeCommit wraps a TxManager: fn runs to completion, then the
// context is cancelled, so the commit fails. This simulates a client
// disconnecting (or the request deadline passing) at the worst possible moment.
type cancelBeforeCommit struct {
	inner  service.TxManager
	cancel context.CancelFunc
}

func (c cancelBeforeCommit) WithinTx(ctx context.Context, fn func(service.Repos) error) error {
	return c.inner.WithinTx(ctx, func(r service.Repos) error {
		if err := fn(r); err != nil {
			return err
		}
		c.cancel()
		return nil
	})
}

func TestCreateTransfer_ContextCancelledBeforeCommit(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, driver string) {
		e := newEnv(t, driver)
		e.seedWallet("a", 100, "SGD")
		e.seedWallet("b", 0, "SGD")
		cmd := service.CreateTransferCommand{IdempotencyKey: "k1", FromWalletID: "a", ToWalletID: "b", Amount: 40}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		svc := service.NewTransferService(cancelBeforeCommit{inner: e.store.TxManager, cancel: cancel})

		_, err := svc.CreateTransfer(ctx, cmd)

		require.ErrorIs(t, err, service.ErrUnavailable)
		e.assertNothingPersisted()
		assert.Equal(t, int64(100), e.balance("a"))

		// The client retries with the same key. Nothing was stored, so the
		// transfer runs for real (it is not a replay).
		res, err := e.transferService().CreateTransfer(context.Background(), cmd)
		require.NoError(t, err)
		assert.False(t, res.Replayed)
		assert.Equal(t, domain.StatusProcessed, res.Transfer.Status)
		assert.Equal(t, int64(60), e.balance("a"))
	})
}

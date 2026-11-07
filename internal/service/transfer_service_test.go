package service_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/celinagupta/wallet-transfer-assignment/internal/domain"
	"github.com/celinagupta/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/celinagupta/wallet-transfer-assignment/internal/service"
	"github.com/celinagupta/wallet-transfer-assignment/internal/testutil/pgtest"
)

func TestMain(m *testing.M) { pgtest.Main(m) }

// setup returns a service over a fresh database seeded with the given wallets.
func setup(t *testing.T, wallets map[string]int64) (*service.TransferService, *pgxpool.Pool) {
	t.Helper()
	pool := pgtest.Pool(t)
	pgtest.SeedWallets(t, pool, wallets)
	svc := service.NewTransferService(postgres.NewStore(pool), slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { assertLedgerConsistent(t, pool, wallets) })
	return svc, pool
}

func transfer(key, from, to string, amount int64) domain.TransferRequest {
	return domain.TransferRequest{IdempotencyKey: key, FromWalletID: from, ToWalletID: to, Amount: amount}
}

// --- Successful transfers ---------------------------------------------------

func TestTransferMovesMoneyAndWritesBalancedLedger(t *testing.T) {
	svc, pool := setup(t, map[string]int64{"wallet_1": 500, "wallet_2": 50})

	res, err := svc.CreateTransfer(context.Background(), transfer("k1", "wallet_1", "wallet_2", 100))

	require.NoError(t, err)
	assert.False(t, res.Replayed)
	assert.Equal(t, domain.StatusProcessed, res.Transfer.Status)
	assert.NotEqual(t, uuid.Nil, res.Transfer.ID, "transfer has an id")
	assert.False(t, res.Transfer.CreatedAt.IsZero())
	assert.Equal(t, int64(400), pgtest.Balance(t, pool, "wallet_1"))
	assert.Equal(t, int64(150), pgtest.Balance(t, pool, "wallet_2"))
	assert.Equal(t, 2, pgtest.Count(t, pool, "ledger_entries"))
}

func TestTransferCanDrainWalletToZero(t *testing.T) {
	svc, pool := setup(t, map[string]int64{"wallet_1": 100, "wallet_2": 0})

	res, err := svc.CreateTransfer(context.Background(), transfer("k1", "wallet_1", "wallet_2", 100))

	require.NoError(t, err)
	assert.Equal(t, domain.StatusProcessed, res.Transfer.Status)
	assert.Equal(t, int64(0), pgtest.Balance(t, pool, "wallet_1"))
}

// --- Failure scenarios ------------------------------------------------------

func TestInsufficientFundsIsRecordedAsFailedAndMovesNothing(t *testing.T) {
	svc, pool := setup(t, map[string]int64{"wallet_1": 10, "wallet_2": 0}) // positive, but less than the amount

	res, err := svc.CreateTransfer(context.Background(), transfer("k1", "wallet_1", "wallet_2", 100))

	require.NoError(t, err, "insufficient funds is an outcome, not an error")
	assert.Equal(t, domain.StatusFailed, res.Transfer.Status)
	assert.Equal(t, domain.FailureInsufficientFunds, res.Transfer.FailureReason)
	assert.Equal(t, int64(10), pgtest.Balance(t, pool, "wallet_1"))
	assert.Equal(t, int64(0), pgtest.Balance(t, pool, "wallet_2"))
	assert.Equal(t, 1, pgtest.Count(t, pool, "transfers"), "the failed attempt is recorded")
	assert.Equal(t, 0, pgtest.Count(t, pool, "ledger_entries"))
}

func TestInsufficientFundsRetryReturnsSameFailureEvenAfterTopUp(t *testing.T) {
	svc, pool := setup(t, map[string]int64{"wallet_1": 10, "wallet_2": 0})
	ctx := context.Background()
	req := transfer("k1", "wallet_1", "wallet_2", 100)
	first, err := svc.CreateTransfer(ctx, req)
	require.NoError(t, err)

	// Top up outside the service; this test's ledger check accounts for it.
	_, err = pool.Exec(ctx, `UPDATE wallets SET balance = 1000 WHERE id = 'wallet_1'`)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `UPDATE wallets SET balance = 10 WHERE id = 'wallet_1'`) })

	retry, err := svc.CreateTransfer(ctx, req)

	require.NoError(t, err)
	assert.True(t, retry.Replayed)
	assert.Equal(t, first.Transfer, retry.Transfer, "the original FAILED result is returned")
	assert.Equal(t, int64(1000), pgtest.Balance(t, pool, "wallet_1"), "retry moved no money")
	assert.Equal(t, 0, pgtest.Count(t, pool, "ledger_entries"))
}

func TestUnknownWalletIsRejectedWithoutSideEffects(t *testing.T) {
	tests := map[string]domain.TransferRequest{
		"unknown source":      transfer("k1", "ghost", "wallet_2", 100),
		"unknown destination": transfer("k1", "wallet_1", "ghost", 100),
	}
	for name, req := range tests {
		t.Run(name, func(t *testing.T) {
			svc, pool := setup(t, map[string]int64{"wallet_1": 500, "wallet_2": 0})

			_, err := svc.CreateTransfer(context.Background(), req)

			require.ErrorIs(t, err, domain.ErrWalletNotFound)
			assert.Equal(t, 2, pgtest.Count(t, pool, "wallets"), "no wallet is created")
			assert.Equal(t, 0, pgtest.Count(t, pool, "transfers"))
			assert.Equal(t, int64(500), pgtest.Balance(t, pool, "wallet_1"))
		})
	}
}

func TestInvalidRequestIsRejectedWithoutSideEffects(t *testing.T) {
	svc, pool := setup(t, map[string]int64{"wallet_1": 500, "wallet_2": 0})

	_, err := svc.CreateTransfer(context.Background(), transfer("k1", "wallet_1", "wallet_2", 0))

	require.ErrorIs(t, err, domain.ErrValidation)
	assert.Equal(t, 0, pgtest.Count(t, pool, "transfers"))
}

// --- Idempotency ------------------------------------------------------------

func TestSameRequestTwiceTransfersOnceAndReplaysOriginal(t *testing.T) {
	svc, pool := setup(t, map[string]int64{"wallet_1": 500, "wallet_2": 0})
	ctx := context.Background()
	req := transfer("k1", "wallet_1", "wallet_2", 100)

	first, err := svc.CreateTransfer(ctx, req)
	require.NoError(t, err)
	second, err := svc.CreateTransfer(ctx, req)
	require.NoError(t, err)

	assert.False(t, first.Replayed)
	assert.True(t, second.Replayed)
	assert.Equal(t, first.Transfer, second.Transfer, "replay returns the original transfer unchanged")
	assert.Equal(t, int64(400), pgtest.Balance(t, pool, "wallet_1"), "debited once")
	assert.Equal(t, int64(100), pgtest.Balance(t, pool, "wallet_2"), "credited once")
	assert.Equal(t, 1, pgtest.Count(t, pool, "transfers"))
	assert.Equal(t, 2, pgtest.Count(t, pool, "ledger_entries"))
}

func TestKeyReusedWithDifferentPayloadIsRejected(t *testing.T) {
	svc, pool := setup(t, map[string]int64{"wallet_1": 500, "wallet_2": 0})
	ctx := context.Background()
	_, err := svc.CreateTransfer(ctx, transfer("k1", "wallet_1", "wallet_2", 100))
	require.NoError(t, err)

	_, err = svc.CreateTransfer(ctx, transfer("k1", "wallet_1", "wallet_2", 999))

	require.ErrorIs(t, err, domain.ErrIdempotencyKeyReused)
	assert.Equal(t, int64(400), pgtest.Balance(t, pool, "wallet_1"), "only the original transfer moved money")
	assert.Equal(t, 1, pgtest.Count(t, pool, "transfers"))
}

func TestRequestsWithoutKeyAreNotDeduplicated(t *testing.T) {
	svc, pool := setup(t, map[string]int64{"wallet_1": 500, "wallet_2": 0})
	ctx := context.Background()
	req := transfer("", "wallet_1", "wallet_2", 100)

	for range 2 {
		res, err := svc.CreateTransfer(ctx, req)
		require.NoError(t, err)
		assert.False(t, res.Replayed)
	}

	assert.Equal(t, int64(300), pgtest.Balance(t, pool, "wallet_1"))
	assert.Equal(t, 2, pgtest.Count(t, pool, "transfers"))
}

// --- Concurrency ------------------------------------------------------------

// runConcurrently starts n calls at once and returns their results in call order.
func runConcurrently(n int, call func(i int) (service.Result, error)) ([]service.Result, []error) {
	results, errs := make([]service.Result, n), make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i], errs[i] = call(i)
		}()
	}
	close(start)
	wg.Wait()
	return results, errs
}

func TestConcurrentRequestsWithSameKeyTransferOnce(t *testing.T) {
	svc, pool := setup(t, map[string]int64{"wallet_1": 500, "wallet_2": 0})
	req := transfer("k1", "wallet_1", "wallet_2", 100)

	results, errs := runConcurrently(20, func(int) (service.Result, error) {
		return svc.CreateTransfer(context.Background(), req)
	})

	executed := 0
	for i := range results {
		require.NoError(t, errs[i])
		assert.Equal(t, results[0].Transfer.ID, results[i].Transfer.ID, "every caller sees the same transfer")
		if !results[i].Replayed {
			executed++
		}
	}
	assert.Equal(t, 1, executed, "exactly one request executed, the rest replayed")
	assert.Equal(t, int64(400), pgtest.Balance(t, pool, "wallet_1"))
	assert.Equal(t, 1, pgtest.Count(t, pool, "transfers"))
}

func TestConcurrentDebitsNeverOverdraw(t *testing.T) {
	svc, pool := setup(t, map[string]int64{"wallet_1": 1000, "wallet_2": 0})

	// 50 transfers of 100 race for a balance that covers exactly 10 of them.
	results, errs := runConcurrently(50, func(i int) (service.Result, error) {
		return svc.CreateTransfer(context.Background(),
			transfer(fmt.Sprintf("debit-%d", i), "wallet_1", "wallet_2", 100))
	})

	processed, failed := 0, 0
	for i := range results {
		require.NoError(t, errs[i])
		switch results[i].Transfer.Status {
		case domain.StatusProcessed:
			processed++
		case domain.StatusFailed:
			failed++
		}
	}
	assert.Equal(t, 10, processed)
	assert.Equal(t, 40, failed)
	assert.Equal(t, int64(0), pgtest.Balance(t, pool, "wallet_1"))
	assert.Equal(t, int64(1000), pgtest.Balance(t, pool, "wallet_2"))
}

func TestConcurrentOverlappingTransfersDoNotDeadlock(t *testing.T) {
	wallets := map[string]int64{"w_a": 1_000_000, "w_b": 1_000_000, "w_c": 1_000_000, "w_d": 1_000_000}
	svc, _ := setup(t, wallets)
	ids := []string{"w_a", "w_b", "w_c", "w_d"}

	// Random pairs in both directions, so transfers share wallets and oppose each
	// other (A->B with B->A). A deadlock or lock timeout would surface as an error.
	_, errs := runConcurrently(200, func(int) (service.Result, error) {
		from := rand.IntN(len(ids))
		to := (from + 1 + rand.IntN(len(ids)-1)) % len(ids)
		return svc.CreateTransfer(context.Background(),
			transfer("", ids[from], ids[to], int64(1+rand.IntN(100))))
	})

	for _, err := range errs {
		require.NoError(t, err)
	}
	// Balances, ledger pairs and conservation of money are checked by assertLedgerConsistent.
}

// --- Invariants -------------------------------------------------------------

// assertLedgerConsistent checks the ledger rules after every test:
//   - no transfer is left PENDING;
//   - every PROCESSED transfer has exactly one DEBIT(from) and one CREDIT(to) of its amount;
//   - FAILED transfers have no entries;
//   - every wallet's balance = opening balance + credits - debits.
func assertLedgerConsistent(t *testing.T, pool *pgxpool.Pool, opening map[string]int64) {
	t.Helper()
	ctx := context.Background()

	var pending int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM transfers WHERE status = 'PENDING'`).Scan(&pending))
	assert.Zero(t, pending, "no transfer may be committed as PENDING")

	var badTransfers int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM transfers t
		WHERE (t.status = 'PROCESSED' AND (
		         (SELECT count(*) FROM ledger_entries e WHERE e.transfer_id = t.id) <> 2
		      OR NOT EXISTS (SELECT 1 FROM ledger_entries e WHERE e.transfer_id = t.id
		                     AND e.entry_type = 'DEBIT' AND e.wallet_id = t.from_wallet_id AND e.amount = t.amount)
		      OR NOT EXISTS (SELECT 1 FROM ledger_entries e WHERE e.transfer_id = t.id
		                     AND e.entry_type = 'CREDIT' AND e.wallet_id = t.to_wallet_id AND e.amount = t.amount)))
		   OR (t.status <> 'PROCESSED' AND EXISTS (SELECT 1 FROM ledger_entries e WHERE e.transfer_id = t.id))`,
	).Scan(&badTransfers))
	assert.Zero(t, badTransfers, "every PROCESSED transfer has one matching DEBIT and CREDIT; others have none")

	rows, err := pool.Query(ctx, `
		SELECT w.id, w.balance,
		       COALESCE(SUM(e.amount) FILTER (WHERE e.entry_type = 'CREDIT'), 0)
		     - COALESCE(SUM(e.amount) FILTER (WHERE e.entry_type = 'DEBIT'), 0)
		FROM wallets w LEFT JOIN ledger_entries e ON e.wallet_id = w.id
		GROUP BY w.id, w.balance`)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var (
			id           string
			balance, net int64
		)
		require.NoError(t, rows.Scan(&id, &balance, &net))
		assert.Equal(t, opening[id]+net, balance, "wallet %s: balance must equal opening + credits - debits", id)
	}
	require.NoError(t, rows.Err())
}

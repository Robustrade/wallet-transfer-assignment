package service

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/AbhishekBadgujar/wallet-transfer-assignment/internals/db"
	"github.com/AbhishekBadgujar/wallet-transfer-assignment/internals/repository"
	"github.com/AbhishekBadgujar/wallet-transfer-assignment/internals/service"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// TestConcurrentTransfers_SameSourceWallet fires N concurrent transfers that
// all debit the SAME source wallet, and asserts:
//  1. the final balance is exactly starting_balance - (N * amount) — no lost
//     updates, no double spending
//  2. every transfer produced exactly one DEBIT and one CREDIT ledger row
//  3. the ledger always balances (sum of debits == sum of credits)
//
// Requires a running Postgres reachable via TEST_DATABASE_URL, with the
// schema already applied. Run with: go test ./tests/... -run Concurrent -v
func TestConcurrentTransfers_SameSourceWallet(t *testing.T) {
	ctx := context.Background()

	dsn := requireTestDSN(t)
	pool, err := db.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	fromWallet, toWallet := seedTwoWallets(t, ctx, pool, 10_000)

	svc := buildTransferService(pool)

	const (
		numTransfers = 20
		amountEach   = 50
	)

	var wg sync.WaitGroup
	errs := make([]error, numTransfers)

	for i := 0; i < numTransfers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := svc.CreateTransfer(ctx, service.CreateTransferInput{
				IdempotencyKey: fmt.Sprintf("concurrent-test-%s-%d", uuid.NewString(), idx),
				FromWalletID:   fromWallet,
				ToWalletID:     toWallet,
				Amount:         amountEach,
			})
			errs[idx] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		require.NoErrorf(t, err, "transfer %d failed", i)
	}

	// 1. Final balances reflect exactly numTransfers * amountEach moved —
	// no lost updates from concurrent writers.
	var fromBalance, toBalance int64
	err = pool.QueryRow(ctx, `SELECT balance FROM wallets WHERE id = $1`, fromWallet).Scan(&fromBalance)
	require.NoError(t, err)
	err = pool.QueryRow(ctx, `SELECT balance FROM wallets WHERE id = $1`, toWallet).Scan(&toBalance)
	require.NoError(t, err)

	require.EqualValues(t, 10_000-(numTransfers*amountEach), fromBalance, "source wallet balance corrupted by concurrent transfers")
	require.EqualValues(t, 10_000+(numTransfers*amountEach), toBalance, "destination wallet balance corrupted by concurrent transfers")

	// 2 & 3. Ledger integrity: exactly 2*numTransfers rows, debits == credits.
	var debitCount, creditCount int
	var debitSum, creditSum int64
	err = pool.QueryRow(ctx, `SELECT count(*), COALESCE(sum(amount),0) FROM ledger_entries WHERE wallet_id = $1 AND type = 'DEBIT'`, fromWallet).Scan(&debitCount, &debitSum)
	require.NoError(t, err)
	err = pool.QueryRow(ctx, `SELECT count(*), COALESCE(sum(amount),0) FROM ledger_entries WHERE wallet_id = $1 AND type = 'CREDIT'`, toWallet).Scan(&creditCount, &creditSum)
	require.NoError(t, err)

	require.Equal(t, numTransfers, debitCount, "expected exactly one DEBIT per transfer")
	require.Equal(t, numTransfers, creditCount, "expected exactly one CREDIT per transfer")
	require.Equal(t, debitSum, creditSum, "ledger does not balance: total debits must equal total credits")
}

// seedTwoWallets inserts two fresh wallets with the given starting balance
// and returns their IDs. Uses random UUIDs so tests can run repeatedly
// without colliding with previous runs.
func seedTwoWallets(t *testing.T, ctx context.Context, pool *pgxpool.Pool, startingBalance int64) (uuid.UUID, uuid.UUID) {
	t.Helper()
	from := uuid.New()
	to := uuid.New()

	_, err := pool.Exec(ctx, `INSERT INTO wallets (id, owner_name, balance) VALUES ($1, 'test-from', $3), ($2, 'test-to', $3)`,
		from, to, startingBalance)
	require.NoError(t, err)

	return from, to
}

func buildTransferService(pool *pgxpool.Pool) *service.TransferService {
	return service.NewTransferService(
		repository.NewTxManager(pool),
		repository.NewWalletRepository(),
		repository.NewTransferRepository(),
		repository.NewLedgerRepository(),
		repository.NewIdempotencyRepository(),
	)
}

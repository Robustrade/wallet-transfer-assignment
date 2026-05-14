package service_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── CreateTransfer ────────────────────────────────────────────────────────────

func TestCreateTransfer_Success(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	from := seedWallet(t, svc, unique("from"), 500)
	to := seedWallet(t, svc, unique("to"), 100)

	transfer, err := svc.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: unique("idem"),
		FromWalletID:   from.ID,
		ToWalletID:     to.ID,
		Amount:         200,
	})

	require.NoError(t, err)
	assert.Equal(t, domain.StatusProcessed, transfer.Status)
	assert.Equal(t, int64(200), transfer.Amount)

	// Verify balances moved correctly.
	fromUpdated, err := svc.GetWallet(ctx, from.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(300), fromUpdated.Balance)

	toUpdated, err := svc.GetWallet(ctx, to.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(300), toUpdated.Balance)
}

func TestCreateTransfer_IdempotencyKey_ReturnsSameTransfer(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	from := seedWallet(t, svc, unique("from"), 1000)
	to := seedWallet(t, svc, unique("to"), 0)
	key := unique("idem")

	req := service.CreateTransferRequest{
		IdempotencyKey: key,
		FromWalletID:   from.ID,
		ToWalletID:     to.ID,
		Amount:         100,
	}

	first, err := svc.CreateTransfer(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusProcessed, first.Status)

	// Send the exact same request a second time.
	second, err := svc.CreateTransfer(ctx, req)
	require.NoError(t, err)

	// Must return the same transfer ID — not create a new one.
	assert.Equal(t, first.ID, second.ID)
	assert.Equal(t, domain.StatusProcessed, second.Status)

	// Balance must only have moved once.
	fromWallet, err := svc.GetWallet(ctx, from.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(900), fromWallet.Balance)
}

func TestCreateTransfer_InsufficientFunds(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	from := seedWallet(t, svc, unique("from"), 50)
	to := seedWallet(t, svc, unique("to"), 0)

	transfer, err := svc.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: unique("idem"),
		FromWalletID:   from.ID,
		ToWalletID:     to.ID,
		Amount:         100,
	})

	assert.ErrorIs(t, err, domain.ErrInsufficientFunds)
	require.NotNil(t, transfer)
	assert.Equal(t, domain.StatusFailed, transfer.Status)

	// Balances must be unchanged.
	fromWallet, err := svc.GetWallet(ctx, from.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(50), fromWallet.Balance)
}

func TestCreateTransfer_WalletNotFound(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	to := seedWallet(t, svc, unique("to"), 0)

	transfer, err := svc.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: unique("idem"),
		FromWalletID:   "non-existent-wallet",
		ToWalletID:     to.ID,
		Amount:         100,
	})

	assert.ErrorIs(t, err, domain.ErrWalletNotFound)
	require.NotNil(t, transfer)
	assert.Equal(t, domain.StatusFailed, transfer.Status)
}

func TestCreateTransfer_SameWallet(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	w := seedWallet(t, svc, unique("w"), 500)

	_, err := svc.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: unique("idem"),
		FromWalletID:   w.ID,
		ToWalletID:     w.ID,
		Amount:         100,
	})

	assert.ErrorIs(t, err, domain.ErrSameWallet)
}

func TestCreateTransfer_InvalidAmount(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	w1 := seedWallet(t, svc, unique("w1"), 500)
	w2 := seedWallet(t, svc, unique("w2"), 0)

	_, err := svc.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: unique("idem"),
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         0,
	})

	assert.ErrorIs(t, err, domain.ErrInvalidAmount)
}

func TestCreateTransfer_LedgerEntries(t *testing.T) {
	svc, pool := newTestService(t)
	ctx := context.Background()

	from := seedWallet(t, svc, unique("from"), 300)
	to := seedWallet(t, svc, unique("to"), 0)

	transfer, err := svc.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: unique("idem"),
		FromWalletID:   from.ID,
		ToWalletID:     to.ID,
		Amount:         150,
	})
	require.NoError(t, err)

	// Verify exactly two ledger entries were created.
	rows, err := pool.Query(ctx,
		`SELECT wallet_id, type, amount FROM ledger_entries WHERE transfer_id = $1 ORDER BY type`,
		transfer.ID,
	)
	require.NoError(t, err)
	defer rows.Close()

	type entry struct {
		walletID  string
		entryType string
		amount    int64
	}
	var entries []entry
	for rows.Next() {
		var e entry
		require.NoError(t, rows.Scan(&e.walletID, &e.entryType, &e.amount))
		entries = append(entries, e)
	}
	require.NoError(t, rows.Err())

	require.Len(t, entries, 2, "expected exactly 2 ledger entries")

	// After ORDER BY type: CREDIT first, then DEBIT.
	assert.Equal(t, to.ID, entries[0].walletID)
	assert.Equal(t, "CREDIT", entries[0].entryType)
	assert.Equal(t, int64(150), entries[0].amount)

	assert.Equal(t, from.ID, entries[1].walletID)
	assert.Equal(t, "DEBIT", entries[1].entryType)
	assert.Equal(t, int64(150), entries[1].amount)
}

// TestCreateTransfer_ConcurrentDebits fires N concurrent transfers from the
// same source wallet and verifies that:
//   - the total deducted never exceeds the initial balance
//   - every committed transfer has matching ledger entries
//   - the ledger balances (sum of CREDITs == sum of DEBITs) hold
func TestCreateTransfer_ConcurrentDebits(t *testing.T) {
	svc, pool := newTestService(t)
	ctx := context.Background()

	const (
		initialBalance = int64(1000)
		transferAmount = int64(150)
		numTransfers   = 10
	)

	from := seedWallet(t, svc, unique("from"), initialBalance)
	to := seedWallet(t, svc, unique("to"), 0)

	var wg sync.WaitGroup
	results := make([]*domain.Transfer, numTransfers)
	errs := make([]error, numTransfers)

	for i := 0; i < numTransfers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = svc.CreateTransfer(ctx, service.CreateTransferRequest{
				IdempotencyKey: unique(fmt.Sprintf("idem-%d", idx)),
				FromWalletID:   from.ID,
				ToWalletID:     to.ID,
				Amount:         transferAmount,
			})
		}(i)
	}
	wg.Wait()

	// Count successes.
	var processed, failed int
	for i := 0; i < numTransfers; i++ {
		require.NotNil(t, results[i])
		if results[i].Status == domain.StatusProcessed {
			processed++
		} else {
			failed++
			assert.ErrorIs(t, errs[i], domain.ErrInsufficientFunds)
		}
	}

	// With a 1000-unit balance and 150-unit transfers, at most 6 can succeed.
	assert.LessOrEqual(t, processed, int(initialBalance/transferAmount))
	t.Logf("concurrent transfers: %d processed, %d failed", processed, failed)

	// Final source balance must equal initialBalance minus (processed * amount).
	fromFinal, err := svc.GetWallet(ctx, from.ID)
	require.NoError(t, err)
	expected := initialBalance - int64(processed)*transferAmount
	assert.Equal(t, expected, fromFinal.Balance, "source wallet balance is incorrect")

	// Ledger must balance: total debits == total credits.
	var totalDebits, totalCredits int64
	rows, err := pool.Query(ctx,
		`SELECT type, SUM(amount) FROM ledger_entries
		 WHERE wallet_id = $1 OR wallet_id = $2
		 GROUP BY type`,
		from.ID, to.ID,
	)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var entryType string
		var total int64
		require.NoError(t, rows.Scan(&entryType, &total))
		if entryType == "DEBIT" {
			totalDebits = total
		} else {
			totalCredits = total
		}
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, totalDebits, totalCredits, "ledger is unbalanced")
}

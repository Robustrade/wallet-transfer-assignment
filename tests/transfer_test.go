package tests

import (
	"context"
	"sync"
	"testing"

	"github.com/RKShukla1997/wallet-transfer-assignment/internal/domain"
	"github.com/RKShukla1997/wallet-transfer-assignment/internal/service"
)

// TestCreateTransfer_HappyPath verifies that a successful transfer debits
// the source wallet, credits the destination wallet, and writes exactly two
// balanced ledger entries.
func TestCreateTransfer_HappyPath(t *testing.T) {
	transferSvc, walletSvc, pool := newTestServices(t)
	resetSchema(t, pool)
	ctx := context.Background()

	from := uniqueID("wallet-from")
	to := uniqueID("wallet-to")
	mustCreateWallet(t, walletSvc, from, 1000)
	mustCreateWallet(t, walletSvc, to, 0)

	transfer, err := transferSvc.CreateTransfer(ctx, service.CreateTransferInput{
		IdempotencyKey: uniqueID("key"),
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         300,
	})
	if err != nil {
		t.Fatalf("CreateTransfer: %v", err)
	}
	if transfer.Status != domain.TransferStatusProcessed {
		t.Fatalf("expected status PROCESSED, got %s (reason: %s)", transfer.Status, transfer.FailureReason)
	}

	fromWallet, err := walletSvc.GetWallet(ctx, from)
	if err != nil {
		t.Fatalf("GetWallet(from): %v", err)
	}
	toWallet, err := walletSvc.GetWallet(ctx, to)
	if err != nil {
		t.Fatalf("GetWallet(to): %v", err)
	}
	if fromWallet.Balance != 700 {
		t.Errorf("expected source balance 700, got %d", fromWallet.Balance)
	}
	if toWallet.Balance != 300 {
		t.Errorf("expected destination balance 300, got %d", toWallet.Balance)
	}

	entries := mustListLedgerEntries(t, pool, transfer.ID)
	if len(entries) != 2 {
		t.Fatalf("expected exactly 2 ledger entries, got %d", len(entries))
	}
	var debitAmount, creditAmount int64
	for _, e := range entries {
		switch e.EntryType {
		case domain.EntryTypeDebit:
			debitAmount = e.Amount
		case domain.EntryTypeCredit:
			creditAmount = e.Amount
		}
	}
	if debitAmount != 300 || creditAmount != 300 {
		t.Errorf("expected balanced debit/credit of 300 each, got debit=%d credit=%d", debitAmount, creditAmount)
	}
}

// TestCreateTransfer_DuplicateIdempotencyKey verifies that submitting the
// same idempotencyKey twice returns the original result and does not
// double-move money or double-write ledger entries.
func TestCreateTransfer_DuplicateIdempotencyKey(t *testing.T) {
	transferSvc, walletSvc, pool := newTestServices(t)
	resetSchema(t, pool)
	ctx := context.Background()

	from := uniqueID("wallet-from")
	to := uniqueID("wallet-to")
	mustCreateWallet(t, walletSvc, from, 1000)
	mustCreateWallet(t, walletSvc, to, 0)

	key := uniqueID("key")
	input := service.CreateTransferInput{
		IdempotencyKey: key,
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         300,
	}

	first, err := transferSvc.CreateTransfer(ctx, input)
	if err != nil {
		t.Fatalf("first CreateTransfer: %v", err)
	}
	second, err := transferSvc.CreateTransfer(ctx, input)
	if err != nil {
		t.Fatalf("second CreateTransfer: %v", err)
	}

	if first.ID != second.ID {
		t.Fatalf("expected replayed transfer to have the same id, got %s and %s", first.ID, second.ID)
	}
	if second.Status != domain.TransferStatusProcessed {
		t.Fatalf("expected replayed status PROCESSED, got %s", second.Status)
	}

	fromWallet, _ := walletSvc.GetWallet(ctx, from)
	if fromWallet.Balance != 700 {
		t.Errorf("expected source balance to only be debited once (700), got %d", fromWallet.Balance)
	}

	entries := mustListLedgerEntries(t, pool, first.ID)
	if len(entries) != 2 {
		t.Fatalf("expected exactly 2 ledger entries after duplicate request, got %d", len(entries))
	}
}

// TestCreateTransfer_ConcurrentDuplicateRequests fires the same
// idempotencyKey concurrently and asserts only one transfer is ever
// actually executed (single debit, single ledger pair).
func TestCreateTransfer_ConcurrentDuplicateRequests(t *testing.T) {
	transferSvc, walletSvc, pool := newTestServices(t)
	resetSchema(t, pool)
	ctx := context.Background()

	from := uniqueID("wallet-from")
	to := uniqueID("wallet-to")
	mustCreateWallet(t, walletSvc, from, 1000)
	mustCreateWallet(t, walletSvc, to, 0)

	key := uniqueID("key")
	const concurrency = 10

	var wg sync.WaitGroup
	results := make([]domain.Transfer, concurrency)
	errs := make([]error, concurrency)
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = transferSvc.CreateTransfer(ctx, service.CreateTransferInput{
				IdempotencyKey: key,
				FromWalletID:   from,
				ToWalletID:     to,
				Amount:         300,
			})
		}(i)
	}
	wg.Wait()

	firstID := ""
	for i, err := range errs {
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
		if firstID == "" {
			firstID = results[i].ID
		} else if results[i].ID != firstID {
			t.Fatalf("expected all concurrent duplicate requests to resolve to the same transfer id")
		}
	}

	fromWallet, _ := walletSvc.GetWallet(ctx, from)
	if fromWallet.Balance != 700 {
		t.Errorf("expected source wallet debited exactly once under concurrency (700), got %d", fromWallet.Balance)
	}

	entries := mustListLedgerEntries(t, pool, firstID)
	if len(entries) != 2 {
		t.Fatalf("expected exactly 2 ledger entries under concurrent duplicate requests, got %d", len(entries))
	}
}

// TestCreateTransfer_ConcurrentDebitsOnSameWallet fires many distinct
// transfers that all debit the same source wallet concurrently, and
// verifies the final balance reflects every debit exactly once with no
// lost updates and no negative balance.
func TestCreateTransfer_ConcurrentDebitsOnSameWallet(t *testing.T) {
	transferSvc, walletSvc, pool := newTestServices(t)
	resetSchema(t, pool)
	ctx := context.Background()

	from := uniqueID("wallet-from")
	to := uniqueID("wallet-to")
	const startingBalance = 10_000
	const perTransfer = 100
	const concurrency = 20

	mustCreateWallet(t, walletSvc, from, startingBalance)
	mustCreateWallet(t, walletSvc, to, 0)

	var wg sync.WaitGroup
	errs := make([]error, concurrency)
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = transferSvc.CreateTransfer(ctx, service.CreateTransferInput{
				IdempotencyKey: uniqueID("key"),
				FromWalletID:   from,
				ToWalletID:     to,
				Amount:         perTransfer,
			})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
	}

	fromWallet, _ := walletSvc.GetWallet(ctx, from)
	toWallet, _ := walletSvc.GetWallet(ctx, to)

	expectedFrom := int64(startingBalance - concurrency*perTransfer)
	expectedTo := int64(concurrency * perTransfer)
	if fromWallet.Balance != expectedFrom {
		t.Errorf("expected source balance %d, got %d (lost update or double spend)", expectedFrom, fromWallet.Balance)
	}
	if toWallet.Balance < 0 {
		t.Errorf("destination balance should never be negative, got %d", toWallet.Balance)
	}
	if toWallet.Balance != expectedTo {
		t.Errorf("expected destination balance %d, got %d", expectedTo, toWallet.Balance)
	}
}

// TestCreateTransfer_InsufficientFunds verifies that a transfer exceeding
// the source wallet's balance is marked FAILED with no balance or ledger
// mutation.
func TestCreateTransfer_InsufficientFunds(t *testing.T) {
	transferSvc, walletSvc, pool := newTestServices(t)
	resetSchema(t, pool)
	ctx := context.Background()

	from := uniqueID("wallet-from")
	to := uniqueID("wallet-to")
	mustCreateWallet(t, walletSvc, from, 50)
	mustCreateWallet(t, walletSvc, to, 0)

	transfer, err := transferSvc.CreateTransfer(ctx, service.CreateTransferInput{
		IdempotencyKey: uniqueID("key"),
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("CreateTransfer: %v", err)
	}
	if transfer.Status != domain.TransferStatusFailed {
		t.Fatalf("expected status FAILED, got %s", transfer.Status)
	}
	if transfer.FailureReason == "" {
		t.Error("expected a failure reason to be recorded")
	}

	fromWallet, _ := walletSvc.GetWallet(ctx, from)
	toWallet, _ := walletSvc.GetWallet(ctx, to)
	if fromWallet.Balance != 50 {
		t.Errorf("expected source balance untouched (50), got %d", fromWallet.Balance)
	}
	if toWallet.Balance != 0 {
		t.Errorf("expected destination balance untouched (0), got %d", toWallet.Balance)
	}

	entries := mustListLedgerEntries(t, pool, transfer.ID)
	if len(entries) != 0 {
		t.Errorf("expected no ledger entries for a failed transfer, got %d", len(entries))
	}
}

// TestCreateTransfer_InvalidRequests verifies structural validation
// rejects malformed requests before any persistence happens.
func TestCreateTransfer_InvalidRequests(t *testing.T) {
	transferSvc, _, pool := newTestServices(t)
	resetSchema(t, pool)
	ctx := context.Background()

	cases := map[string]service.CreateTransferInput{
		"missing idempotency key": {FromWalletID: "a", ToWalletID: "b", Amount: 10},
		"same wallet":             {IdempotencyKey: "k1", FromWalletID: "a", ToWalletID: "a", Amount: 10},
		"zero amount":             {IdempotencyKey: "k2", FromWalletID: "a", ToWalletID: "b", Amount: 0},
		"negative amount":         {IdempotencyKey: "k3", FromWalletID: "a", ToWalletID: "b", Amount: -10},
	}

	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := transferSvc.CreateTransfer(ctx, in); err == nil {
				t.Fatalf("expected a validation error for case %q", name)
			}
		})
	}
}

package service_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/jaiminbhaduri/wallet-transfer-assignment/internal/domain"
	"github.com/jaiminbhaduri/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/jaiminbhaduri/wallet-transfer-assignment/internal/service"
)

func TestCreateTransfer_Success(t *testing.T) {
	pool := testPool(t)
	svc := service.NewTransferService(pool)
	ctx := context.Background()

	from := seedWallet(t, pool, 1000)
	to := seedWallet(t, pool, 500)

	tr, err := svc.CreateTransfer(ctx, domain.CreateTransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         200,
	})
	if err != nil {
		t.Fatalf("CreateTransfer: %v", err)
	}
	if tr.Status != domain.TransferProcessed {
		t.Fatalf("status = %s, want PROCESSED", tr.Status)
	}

	fromWallet, err := svc.GetWalletBalance(ctx, from)
	if err != nil {
		t.Fatal(err)
	}
	toWallet, err := svc.GetWalletBalance(ctx, to)
	if err != nil {
		t.Fatal(err)
	}
	if fromWallet.Balance != 800 {
		t.Errorf("from balance = %d, want 800", fromWallet.Balance)
	}
	if toWallet.Balance != 700 {
		t.Errorf("to balance = %d, want 700", toWallet.Balance)
	}

	entries, err := postgres.NewLedgerRepo(pool).ListByTransferID(ctx, tr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d ledger entries, want 2", len(entries))
	}
	var debit, credit *domain.LedgerEntry
	for i := range entries {
		switch entries[i].Type {
		case domain.EntryDebit:
			debit = &entries[i]
		case domain.EntryCredit:
			credit = &entries[i]
		}
	}
	if debit == nil || credit == nil {
		t.Fatalf("expected one DEBIT and one CREDIT entry, got %+v", entries)
	}
	if debit.Amount != 200 || credit.Amount != 200 {
		t.Errorf("ledger amounts mismatch: debit=%d credit=%d, want 200/200", debit.Amount, credit.Amount)
	}
	if debit.WalletID != from || credit.WalletID != to {
		t.Errorf("ledger wallet ids mismatch: debit.WalletID=%s credit.WalletID=%s", debit.WalletID, credit.WalletID)
	}
}

func TestCreateTransfer_IdempotentReplay(t *testing.T) {
	pool := testPool(t)
	svc := service.NewTransferService(pool)
	ctx := context.Background()

	from := seedWallet(t, pool, 1000)
	to := seedWallet(t, pool, 500)

	req := domain.CreateTransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         300,
	}

	first, err := svc.CreateTransfer(ctx, req)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Same idempotency key, same request: must replay, not re-execute.
	second, err := svc.CreateTransfer(ctx, req)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if first.ID != second.ID {
		t.Fatalf("replay returned a different transfer id: %s vs %s", first.ID, second.ID)
	}
	if second.Status != domain.TransferProcessed {
		t.Fatalf("replay status = %s, want PROCESSED", second.Status)
	}

	fromWallet, err := svc.GetWalletBalance(ctx, from)
	if err != nil {
		t.Fatal(err)
	}
	if fromWallet.Balance != 700 {
		t.Errorf("balance debited more than once: got %d, want 700 (debited exactly once)", fromWallet.Balance)
	}

	entries, err := postgres.NewLedgerRepo(pool).ListByTransferID(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d ledger entries after replay, want 2 (no duplicate side effects)", len(entries))
	}
}

func TestCreateTransfer_InsufficientFunds(t *testing.T) {
	pool := testPool(t)
	svc := service.NewTransferService(pool)
	ctx := context.Background()

	from := seedWallet(t, pool, 50)
	to := seedWallet(t, pool, 0)

	tr, err := svc.CreateTransfer(ctx, domain.CreateTransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("CreateTransfer: %v", err)
	}
	if tr.Status != domain.TransferFailed {
		t.Fatalf("status = %s, want FAILED", tr.Status)
	}
	if tr.FailureReason == nil || *tr.FailureReason != domain.ErrInsufficientFunds.Error() {
		t.Errorf("failure reason = %v, want %q", tr.FailureReason, domain.ErrInsufficientFunds.Error())
	}

	fromWallet, err := svc.GetWalletBalance(ctx, from)
	if err != nil {
		t.Fatal(err)
	}
	if fromWallet.Balance != 50 {
		t.Errorf("balance changed on a failed transfer: got %d, want 50 (unchanged)", fromWallet.Balance)
	}

	entries, err := postgres.NewLedgerRepo(pool).ListByTransferID(ctx, tr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no ledger entries for a failed transfer, got %d", len(entries))
	}
}

func TestCreateTransfer_WalletNotFound(t *testing.T) {
	pool := testPool(t)
	svc := service.NewTransferService(pool)
	ctx := context.Background()

	from := seedWallet(t, pool, 1000)

	_, err := svc.CreateTransfer(ctx, domain.CreateTransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   from,
		ToWalletID:     "does-not-exist-" + uuid.NewString(),
		Amount:         100,
	})
	if err == nil {
		t.Fatal("expected an error for a nonexistent destination wallet, got nil")
	}
}

// TestCreateTransfer_Concurrent fires many concurrent transfers from the
// same source wallet and asserts the final balance reflects every one of
// them exactly once — the scenario the assignment calls out explicitly:
// "two transfers attempt to debit the same wallet simultaneously".
func TestCreateTransfer_Concurrent(t *testing.T) {
	pool := testPool(t)
	svc := service.NewTransferService(pool)
	ctx := context.Background()

	from := seedWallet(t, pool, 10_000)
	to := seedWallet(t, pool, 0)

	const n = 20
	const amount = 100

	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := svc.CreateTransfer(ctx, domain.CreateTransferRequest{
				IdempotencyKey: fmt.Sprintf("concurrent-%s-%d", from, i),
				FromWalletID:   from,
				ToWalletID:     to,
				Amount:         amount,
			})
			errs[i] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("transfer %d failed: %v", i, err)
		}
	}

	fromWallet, err := svc.GetWalletBalance(ctx, from)
	if err != nil {
		t.Fatal(err)
	}
	toWallet, err := svc.GetWalletBalance(ctx, to)
	if err != nil {
		t.Fatal(err)
	}

	wantFrom := int64(10_000 - n*amount)
	wantTo := int64(n * amount)
	if fromWallet.Balance != wantFrom {
		t.Errorf("from balance = %d, want %d (a mismatch here means a lost update)", fromWallet.Balance, wantFrom)
	}
	if toWallet.Balance != wantTo {
		t.Errorf("to balance = %d, want %d", toWallet.Balance, wantTo)
	}
}

// TestCreateTransfer_ConcurrentOppositeDirections fires transfers in both
// directions between the same wallet pair at once. Without a fixed lock
// acquisition order, one goroutine could hold wallet A's lock while waiting
// on B, and another could hold B's lock while waiting on A — a classic
// deadlock. This test proves the canonical (sorted-id) locking order in
// TransferService.CreateTransfer avoids it.
func TestCreateTransfer_ConcurrentOppositeDirections(t *testing.T) {
	pool := testPool(t)
	svc := service.NewTransferService(pool)
	ctx := context.Background()

	walletA := seedWallet(t, pool, 5000)
	walletB := seedWallet(t, pool, 5000)

	const n = 15
	const amount = 10

	var wg sync.WaitGroup
	errs := make([]error, n*2)

	for i := 0; i < n; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			_, err := svc.CreateTransfer(ctx, domain.CreateTransferRequest{
				IdempotencyKey: fmt.Sprintf("a-to-b-%s-%s-%d", walletA, walletB, i),
				FromWalletID:   walletA,
				ToWalletID:     walletB,
				Amount:         amount,
			})
			errs[i] = err
		}(i)
		go func(i int) {
			defer wg.Done()
			_, err := svc.CreateTransfer(ctx, domain.CreateTransferRequest{
				IdempotencyKey: fmt.Sprintf("b-to-a-%s-%s-%d", walletA, walletB, i),
				FromWalletID:   walletB,
				ToWalletID:     walletA,
				Amount:         amount,
			})
			errs[n+i] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("transfer %d failed (possible deadlock or race): %v", i, err)
		}
	}

	walletAAfter, err := svc.GetWalletBalance(ctx, walletA)
	if err != nil {
		t.Fatal(err)
	}
	walletBAfter, err := svc.GetWalletBalance(ctx, walletB)
	if err != nil {
		t.Fatal(err)
	}

	// Equal amounts flowed both ways, so both balances should net back to
	// their starting point.
	if walletAAfter.Balance != 5000 || walletBAfter.Balance != 5000 {
		t.Errorf("balances did not net out: A=%d B=%d, want 5000/5000", walletAAfter.Balance, walletBAfter.Balance)
	}
}

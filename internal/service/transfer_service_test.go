package service_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"Wallet_Transfer_Service/internal/domain"
	"Wallet_Transfer_Service/internal/repository/memory"
	"Wallet_Transfer_Service/internal/service"
)

func newFixture(t *testing.T) (*service.TransferService, *service.WalletService) {
	t.Helper()
	store := memory.NewStore()
	return service.NewTransferService(store), service.NewWalletService(store)
}

func mustCreateWallet(t *testing.T, ws *service.WalletService, id string, balance int64) {
	t.Helper()
	if _, err := ws.CreateWallet(context.Background(), id, balance); err != nil {
		t.Fatalf("create wallet %s: %v", id, err)
	}
}

func TestCreateTransfer_Success(t *testing.T) {
	ts, ws := newFixture(t)
	ctx := context.Background()
	mustCreateWallet(t, ws, "wallet_1", 500)
	mustCreateWallet(t, ws, "wallet_2", 100)

	result, replayed, err := ts.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "key-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("CreateTransfer: %v", err)
	}
	if replayed {
		t.Fatalf("expected fresh transfer, got replayed")
	}
	if result.State != domain.TransferProcessed {
		t.Fatalf("expected PROCESSED, got %s", result.State)
	}

	from, _ := ws.GetWallet(ctx, "wallet_1")
	to, _ := ws.GetWallet(ctx, "wallet_2")
	if from.Balance != 400 {
		t.Fatalf("expected from balance 400, got %d", from.Balance)
	}
	if to.Balance != 200 {
		t.Fatalf("expected to balance 200, got %d", to.Balance)
	}

	detail, err := ts.GetTransfer(ctx, result.TransferID)
	if err != nil {
		t.Fatalf("GetTransfer: %v", err)
	}
	if len(detail.Entries) != 2 {
		t.Fatalf("expected 2 ledger entries, got %d", len(detail.Entries))
	}
	var debit, credit *domain.LedgerEntry
	for _, e := range detail.Entries {
		switch e.EntryType {
		case domain.EntryDebit:
			debit = e
		case domain.EntryCredit:
			credit = e
		}
	}
	if debit == nil || debit.WalletID != "wallet_1" || debit.Amount != 100 {
		t.Fatalf("bad debit entry: %+v", debit)
	}
	if credit == nil || credit.WalletID != "wallet_2" || credit.Amount != 100 {
		t.Fatalf("bad credit entry: %+v", credit)
	}
}

func TestCreateTransfer_IdempotentReplay(t *testing.T) {
	ts, ws := newFixture(t)
	ctx := context.Background()
	mustCreateWallet(t, ws, "wallet_1", 500)
	mustCreateWallet(t, ws, "wallet_2", 100)

	req := service.CreateTransferRequest{
		IdempotencyKey: "dup-key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}

	first, _, err := ts.CreateTransfer(ctx, req)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	second, replayed, err := ts.CreateTransfer(ctx, req)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if !replayed {
		t.Fatalf("expected second call to be a replay")
	}
	if first.TransferID != second.TransferID {
		t.Fatalf("expected same transfer id, got %s and %s", first.TransferID, second.TransferID)
	}

	from, _ := ws.GetWallet(ctx, "wallet_1")
	if from.Balance != 400 {
		t.Fatalf("duplicate request must not debit twice: got balance %d", from.Balance)
	}

	detail, err := ts.GetTransfer(ctx, first.TransferID)
	if err != nil {
		t.Fatalf("GetTransfer: %v", err)
	}
	if len(detail.Entries) != 2 {
		t.Fatalf("duplicate request must not create duplicate ledger entries: got %d entries", len(detail.Entries))
	}
}

func TestCreateTransfer_IdempotencyKeyConflict(t *testing.T) {
	ts, ws := newFixture(t)
	ctx := context.Background()
	mustCreateWallet(t, ws, "wallet_1", 500)
	mustCreateWallet(t, ws, "wallet_2", 100)
	mustCreateWallet(t, ws, "wallet_3", 100)

	req := service.CreateTransferRequest{IdempotencyKey: "shared-key", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100}
	if _, _, err := ts.CreateTransfer(ctx, req); err != nil {
		t.Fatalf("first call: %v", err)
	}

	req.ToWalletID = "wallet_3"
	_, _, err := ts.CreateTransfer(ctx, req)
	if err != domain.ErrIdempotencyKeyConflict {
		t.Fatalf("expected ErrIdempotencyKeyConflict, got %v", err)
	}
}

func TestCreateTransfer_InsufficientFunds(t *testing.T) {
	ts, ws := newFixture(t)
	ctx := context.Background()
	mustCreateWallet(t, ws, "wallet_1", 50)
	mustCreateWallet(t, ws, "wallet_2", 0)

	result, _, err := ts.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "key-insufficient",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("expected a FAILED transfer result, not an error: %v", err)
	}
	if result.State != domain.TransferFailed {
		t.Fatalf("expected FAILED, got %s", result.State)
	}

	from, _ := ws.GetWallet(ctx, "wallet_1")
	if from.Balance != 50 {
		t.Fatalf("balance must be unchanged after a failed transfer, got %d", from.Balance)
	}

	detail, err := ts.GetTransfer(ctx, result.TransferID)
	if err != nil {
		t.Fatalf("GetTransfer: %v", err)
	}
	if len(detail.Entries) != 0 {
		t.Fatalf("a FAILED transfer must not produce ledger entries, got %d", len(detail.Entries))
	}
}

func TestCreateTransfer_WalletNotFound(t *testing.T) {
	ts, ws := newFixture(t)
	ctx := context.Background()
	mustCreateWallet(t, ws, "wallet_1", 500)

	_, _, err := ts.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "key-missing",
		FromWalletID:   "wallet_1",
		ToWalletID:     "does_not_exist",
		Amount:         10,
	})
	if err != domain.ErrWalletNotFound {
		t.Fatalf("expected ErrWalletNotFound, got %v", err)
	}
}

func TestCreateTransfer_ConcurrentSameSourceWallet(t *testing.T) {
	ts, ws := newFixture(t)
	ctx := context.Background()
	mustCreateWallet(t, ws, "wallet_1", 500)
	mustCreateWallet(t, ws, "wallet_2", 0)

	const attempts = 20
	const amount = 50 // only 10 of 20 attempts can succeed

	var wg sync.WaitGroup
	results := make([]*service.TransferResult, attempts)
	errs := make([]error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, _, err := ts.CreateTransfer(ctx, service.CreateTransferRequest{
				IdempotencyKey: fmt.Sprintf("concurrent-key-%d", i),
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
				Amount:         amount,
			})
			results[i], errs[i] = res, err
		}(i)
	}
	wg.Wait()

	var processed, failed int
	for i, err := range errs {
		if err != nil {
			t.Fatalf("attempt %d returned unexpected error: %v", i, err)
		}
		switch results[i].State {
		case domain.TransferProcessed:
			processed++
		case domain.TransferFailed:
			failed++
		default:
			t.Fatalf("unexpected state %s", results[i].State)
		}
	}
	if processed != 10 {
		t.Fatalf("expected exactly 10 processed transfers, got %d", processed)
	}
	if failed != 10 {
		t.Fatalf("expected exactly 10 failed transfers, got %d", failed)
	}

	from, _ := ws.GetWallet(ctx, "wallet_1")
	to, _ := ws.GetWallet(ctx, "wallet_2")
	if from.Balance != 0 {
		t.Fatalf("expected source wallet drained to 0, got %d", from.Balance)
	}
	if to.Balance != 500 {
		t.Fatalf("expected dest wallet at 500, got %d", to.Balance)
	}
}

// TestCreateTransfer_ConcurrentDuplicateRequests fires the same
// idempotency key concurrently many times and asserts only one transfer
// is ever created and the debit only ever happens once.
func TestCreateTransfer_ConcurrentDuplicateRequests(t *testing.T) {
	ts, ws := newFixture(t)
	ctx := context.Background()
	mustCreateWallet(t, ws, "wallet_1", 500)
	mustCreateWallet(t, ws, "wallet_2", 0)

	const attempts = 25
	req := service.CreateTransferRequest{
		IdempotencyKey: "same-key-race",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}

	var wg sync.WaitGroup
	transferIDs := make([]string, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for {
				res, _, err := ts.CreateTransfer(ctx, req)
				if err == domain.ErrIdempotencyKeyInUse {
					continue // still being claimed elsewhere; retry like a real client would
				}
				if err != nil {
					t.Errorf("attempt %d: unexpected error %v", i, err)
					return
				}
				transferIDs[i] = res.TransferID
				return
			}
		}(i)
	}
	wg.Wait()

	first := transferIDs[0]
	for i, id := range transferIDs {
		if id != first {
			t.Fatalf("attempt %d got a different transfer id (%s vs %s); duplicate transfer was created", i, id, first)
		}
	}

	from, _ := ws.GetWallet(ctx, "wallet_1")
	if from.Balance != 400 {
		t.Fatalf("expected exactly one debit of 100, got balance %d", from.Balance)
	}
}

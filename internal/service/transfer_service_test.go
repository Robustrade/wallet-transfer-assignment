package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wallet-transfer-assignment/wallet-transfer/internal/domain"
	"github.com/wallet-transfer-assignment/wallet-transfer/internal/service"
	"github.com/wallet-transfer-assignment/wallet-transfer/internal/testutil"
)

// newTestServices wires a TransferService and WalletService against an
// isolated repository mock.
func newTestServices(t *testing.T) (*service.TransferService, *service.WalletService) {
	t.Helper()
	uow := testutil.NewMockUnitOfWork()
	return service.NewTransferService(uow), service.NewWalletService(uow)
}

func TestTransferServiceOptions(t *testing.T) {
	wantTime := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	transfers, wallets := func() (*service.TransferService, *service.WalletService) {
		uow := testutil.NewMockUnitOfWork()
		opts := []service.Option{
			service.WithClock(func() time.Time { return wantTime }),
			service.WithIDGenerator(func() string { return "fixed-id" }),
		}
		return service.NewTransferService(uow, opts...), service.NewWalletService(uow, opts...)
	}()
	seedWallet(t, wallets, "wallet_1", 100)
	seedWallet(t, wallets, "wallet_2", 0)
	result, err := transfers.CreateTransfer(context.Background(), service.CreateTransferInput{
		IdempotencyKey: "options",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         10,
	})
	if err != nil {
		t.Fatalf("CreateTransfer: %v", err)
	}
	if result.Transfer.ID != "fixed-id" || !result.Transfer.CreatedAt.Equal(wantTime) {
		t.Fatalf("transfer = %#v, want fixed ID and timestamp", result.Transfer)
	}
}

func seedWallet(t *testing.T, wallets *service.WalletService, id string, balance int64) {
	t.Helper()
	if _, err := wallets.CreateWallet(context.Background(), service.CreateWalletInput{ID: id, InitialBalance: balance}); err != nil {
		t.Fatalf("seed wallet %s: %v", id, err)
	}
}

func TestCreateTransfer_Success(t *testing.T) {
	transfers, wallets := newTestServices(t)
	ctx := context.Background()
	seedWallet(t, wallets, "wallet_1", 500)
	seedWallet(t, wallets, "wallet_2", 100)

	result, err := transfers.CreateTransfer(ctx, service.CreateTransferInput{
		IdempotencyKey: "key-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("CreateTransfer: %v", err)
	}
	if result.Transfer.State != domain.TransferProcessed {
		t.Fatalf("state = %s, want PROCESSED", result.Transfer.State)
	}
	if len(result.LedgerEntries) != 2 {
		t.Fatalf("got %d ledger entries, want 2", len(result.LedgerEntries))
	}

	from, err := wallets.GetWallet(ctx, "wallet_1")
	if err != nil {
		t.Fatalf("GetWallet(wallet_1): %v", err)
	}
	if from.Balance != 400 {
		t.Errorf("wallet_1 balance = %d, want 400", from.Balance)
	}

	to, err := wallets.GetWallet(ctx, "wallet_2")
	if err != nil {
		t.Fatalf("GetWallet(wallet_2): %v", err)
	}
	if to.Balance != 200 {
		t.Errorf("wallet_2 balance = %d, want 200", to.Balance)
	}

	var debit, credit *domain.LedgerEntry
	for i := range result.LedgerEntries {
		e := &result.LedgerEntries[i]
		switch e.Type {
		case domain.LedgerEntryDebit:
			debit = e
		case domain.LedgerEntryCredit:
			credit = e
		}
	}
	if debit == nil || credit == nil {
		t.Fatal("expected one debit and one credit entry")
	}
	if debit.Amount != credit.Amount {
		t.Errorf("debit amount %d != credit amount %d, ledger does not balance", debit.Amount, credit.Amount)
	}
	if debit.WalletID != "wallet_1" || credit.WalletID != "wallet_2" {
		t.Errorf("ledger entries reference wrong wallets: debit=%s credit=%s", debit.WalletID, credit.WalletID)
	}
}

func TestCreateTransfer_InsufficientFunds(t *testing.T) {
	transfers, wallets := newTestServices(t)
	ctx := context.Background()
	seedWallet(t, wallets, "wallet_1", 50)
	seedWallet(t, wallets, "wallet_2", 0)

	result, err := transfers.CreateTransfer(ctx, service.CreateTransferInput{
		IdempotencyKey: "key-insufficient",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("CreateTransfer: %v", err)
	}
	if result.Transfer.State != domain.TransferFailed {
		t.Fatalf("state = %s, want FAILED", result.Transfer.State)
	}
	if result.Transfer.FailureReason == "" {
		t.Error("expected a failure reason to be recorded")
	}
	if len(result.LedgerEntries) != 0 {
		t.Errorf("expected no ledger entries for a failed transfer, got %d", len(result.LedgerEntries))
	}

	from, _ := wallets.GetWallet(ctx, "wallet_1")
	if from.Balance != 50 {
		t.Errorf("wallet_1 balance changed on a failed transfer: %d, want 50", from.Balance)
	}
}

func TestCreateTransfer_IdempotentReplay(t *testing.T) {
	transfers, wallets := newTestServices(t)
	ctx := context.Background()
	seedWallet(t, wallets, "wallet_1", 500)
	seedWallet(t, wallets, "wallet_2", 0)

	in := service.CreateTransferInput{
		IdempotencyKey: "dup-key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}

	first, err := transfers.CreateTransfer(ctx, in)
	if err != nil {
		t.Fatalf("first CreateTransfer: %v", err)
	}
	if first.Replayed {
		t.Fatal("first request should not be marked as replayed")
	}

	second, err := transfers.CreateTransfer(ctx, in)
	if err != nil {
		t.Fatalf("second CreateTransfer: %v", err)
	}
	if !second.Replayed {
		t.Fatal("second request with the same idempotencyKey should be marked as replayed")
	}
	if second.Transfer.ID != first.Transfer.ID {
		t.Errorf("replayed transfer ID = %s, want %s", second.Transfer.ID, first.Transfer.ID)
	}

	from, _ := wallets.GetWallet(ctx, "wallet_1")
	if from.Balance != 400 {
		t.Errorf("wallet_1 balance = %d, want 400 (duplicate must not double-debit)", from.Balance)
	}
}

func TestCreateTransfer_IdempotencyKeyConflict(t *testing.T) {
	transfers, wallets := newTestServices(t)
	ctx := context.Background()
	seedWallet(t, wallets, "wallet_1", 500)
	seedWallet(t, wallets, "wallet_2", 0)

	_, err := transfers.CreateTransfer(ctx, service.CreateTransferInput{
		IdempotencyKey: "shared-key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("first CreateTransfer: %v", err)
	}

	_, err = transfers.CreateTransfer(ctx, service.CreateTransferInput{
		IdempotencyKey: "shared-key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         200, // different amount, same key
	})
	if err == nil {
		t.Fatal("expected an error reusing the same idempotencyKey with a different amount")
	}
	if !errors.Is(err, domain.ErrIdempotencyKeyConflict) {
		t.Errorf("expected ErrIdempotencyKeyConflict, got %v", err)
	}
}

func TestCreateTransfer_SameWallet(t *testing.T) {
	transfers, wallets := newTestServices(t)
	ctx := context.Background()
	seedWallet(t, wallets, "wallet_1", 500)

	_, err := transfers.CreateTransfer(ctx, service.CreateTransferInput{
		IdempotencyKey: "same-wallet",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_1",
		Amount:         10,
	})
	if !errors.Is(err, domain.ErrSameWallet) {
		t.Fatalf("err = %v, want ErrSameWallet", err)
	}
}

func TestCreateTransfer_InvalidAmount(t *testing.T) {
	transfers, wallets := newTestServices(t)
	ctx := context.Background()
	seedWallet(t, wallets, "wallet_1", 500)
	seedWallet(t, wallets, "wallet_2", 0)

	_, err := transfers.CreateTransfer(ctx, service.CreateTransferInput{
		IdempotencyKey: "bad-amount",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         0,
	})
	if !errors.Is(err, domain.ErrInvalidAmount) {
		t.Fatalf("err = %v, want ErrInvalidAmount", err)
	}
}

func TestCreateTransfer_WalletNotFound_KeyNotConsumed(t *testing.T) {
	transfers, wallets := newTestServices(t)
	ctx := context.Background()
	seedWallet(t, wallets, "wallet_1", 500)

	_, err := transfers.CreateTransfer(ctx, service.CreateTransferInput{
		IdempotencyKey: "retry-key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "does-not-exist",
		Amount:         10,
	})
	if !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("err = %v, want ErrWalletNotFound", err)
	}
	if err.Error() != `wallet "does-not-exist" not found` {
		t.Fatalf("err = %q, want missing wallet ID", err)
	}

	// The idempotency key must not be consumed by a request that failed
	// before a transfer was durably recorded, so the client can correct the
	// destination wallet and retry with the same key.
	seedWallet(t, wallets, "wallet_2", 0)
	result, err := transfers.CreateTransfer(ctx, service.CreateTransferInput{
		IdempotencyKey: "retry-key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         10,
	})
	if err != nil {
		t.Fatalf("retry after fixing wallet id: %v", err)
	}
	if result.Replayed {
		t.Fatal("retry with a corrected request should not be treated as a replay")
	}
}

func TestCreateTransfer_IdempotencyKeyRequired(t *testing.T) {
	transfers, wallets := newTestServices(t)
	ctx := context.Background()
	seedWallet(t, wallets, "wallet_1", 500)
	seedWallet(t, wallets, "wallet_2", 0)

	_, err := transfers.CreateTransfer(ctx, service.CreateTransferInput{
		FromWalletID: "wallet_1",
		ToWalletID:   "wallet_2",
		Amount:       10,
	})
	if !errors.Is(err, domain.ErrIdempotencyKeyRequired) {
		t.Fatalf("err = %v, want ErrIdempotencyKeyRequired", err)
	}
}

func TestTransferService_ReadTransferAndHistory(t *testing.T) {
	transfers, wallets := newTestServices(t)
	ctx := context.Background()
	seedWallet(t, wallets, "wallet_1", 500)
	seedWallet(t, wallets, "wallet_2", 0)

	created, err := transfers.CreateTransfer(ctx, service.CreateTransferInput{
		IdempotencyKey: "read-path",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("CreateTransfer: %v", err)
	}

	found, err := transfers.GetTransfer(ctx, created.Transfer.ID)
	if err != nil {
		t.Fatalf("GetTransfer: %v", err)
	}
	if found.Transfer.ID != created.Transfer.ID || len(found.LedgerEntries) != 2 {
		t.Fatalf("GetTransfer returned ID %q and %d ledger entries", found.Transfer.ID, len(found.LedgerEntries))
	}

	history, err := wallets.ListTransferHistory(ctx, "wallet_1")
	if err != nil {
		t.Fatalf("ListTransferHistory: %v", err)
	}
	if len(history) != 1 || history[0].ID != created.Transfer.ID {
		t.Fatalf("history = %#v, want one transfer %q", history, created.Transfer.ID)
	}

	if _, err := transfers.GetTransfer(ctx, "missing"); !errors.Is(err, domain.ErrTransferNotFound) {
		t.Fatalf("missing transfer error = %v, want ErrTransferNotFound", err)
	}
	if _, err := wallets.ListTransferHistory(ctx, "missing"); !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("missing wallet history error = %v, want ErrWalletNotFound", err)
	}
}

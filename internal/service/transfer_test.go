package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/sumiyapuletipalli/wallet-transfer-assignment/internal/domain"
	"github.com/sumiyapuletipalli/wallet-transfer-assignment/internal/repository"
)

func TestCreateTransferProcessesBalancesAndLedger(t *testing.T) {
	tx := newFakeTransaction(true)
	tx.wallets["wallet-1"] = domain.Wallet{ID: "wallet-1", Balance: 200}
	tx.wallets["wallet-2"] = domain.Wallet{ID: "wallet-2", Balance: 50}
	service := testService(tx)

	result, err := service.CreateTransfer(context.Background(), TransferCommand{
		IdempotencyKey: "key-1",
		FromWalletID:   "wallet-1",
		ToWalletID:     "wallet-2",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("create transfer: %v", err)
	}
	if result.Transfer.Status != domain.TransferStatusProcessed {
		t.Fatalf("status = %q, want PROCESSED", result.Transfer.Status)
	}
	if got := tx.wallets["wallet-1"].Balance; got != 100 {
		t.Fatalf("source balance = %d, want 100", got)
	}
	if got := tx.wallets["wallet-2"].Balance; got != 150 {
		t.Fatalf("destination balance = %d, want 150", got)
	}
	if len(tx.entries) != 2 {
		t.Fatalf("ledger entries = %d, want 2", len(tx.entries))
	}
	if tx.entries[0].Type != domain.LedgerEntryDebit || tx.entries[1].Type != domain.LedgerEntryCredit {
		t.Fatalf("unexpected ledger entries: %#v", tx.entries)
	}
}

func TestCreateTransferInsufficientBalanceIsTerminalFailure(t *testing.T) {
	tx := newFakeTransaction(true)
	tx.wallets["wallet-1"] = domain.Wallet{ID: "wallet-1", Balance: 20}
	tx.wallets["wallet-2"] = domain.Wallet{ID: "wallet-2", Balance: 50}

	result, err := testService(tx).CreateTransfer(context.Background(), TransferCommand{
		IdempotencyKey: "key-1",
		FromWalletID:   "wallet-1",
		ToWalletID:     "wallet-2",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("create transfer: %v", err)
	}
	if result.Transfer.Status != domain.TransferStatusFailed {
		t.Fatalf("status = %q, want FAILED", result.Transfer.Status)
	}
	if result.Transfer.FailureCode != domain.FailureCodeInsufficientBalance {
		t.Fatalf("failure code = %q, want INSUFFICIENT_BALANCE", result.Transfer.FailureCode)
	}
	if tx.wallets["wallet-1"].Balance != 20 || tx.wallets["wallet-2"].Balance != 50 {
		t.Fatal("failed transfer changed wallet balances")
	}
	if len(tx.entries) != 0 {
		t.Fatalf("failed transfer created %d ledger entries", len(tx.entries))
	}
}

func TestCreateTransferMissingWalletIsTerminalFailure(t *testing.T) {
	tx := newFakeTransaction(true)
	tx.wallets["wallet-1"] = domain.Wallet{ID: "wallet-1", Balance: 200}

	result, err := testService(tx).CreateTransfer(context.Background(), TransferCommand{
		IdempotencyKey: "key-1",
		FromWalletID:   "wallet-1",
		ToWalletID:     "wallet-2",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("create transfer: %v", err)
	}
	if result.Transfer.Status != domain.TransferStatusFailed {
		t.Fatalf("status = %q, want FAILED", result.Transfer.Status)
	}
	if result.Transfer.FailureCode != domain.FailureCodeWalletNotFound {
		t.Fatalf("failure code = %q, want WALLET_NOT_FOUND", result.Transfer.FailureCode)
	}
	if tx.wallets["wallet-1"].Balance != 200 {
		t.Fatal("missing wallet failure changed wallet balance")
	}
	if len(tx.entries) != 0 {
		t.Fatalf("missing wallet failure created %d ledger entries", len(tx.entries))
	}
}

func TestCreateTransferReturnsOriginalResultForDuplicate(t *testing.T) {
	tx := newFakeTransaction(false)
	command := TransferCommand{
		IdempotencyKey: "key-1",
		FromWalletID:   "wallet-1",
		ToWalletID:     "wallet-2",
		Amount:         100,
	}
	original := domain.NewPendingTransfer(
		"original-transfer",
		command.IdempotencyKey,
		command.FromWalletID,
		command.ToWalletID,
		command.Amount,
		time.Now(),
	)
	if err := original.MarkProcessed(time.Now()); err != nil {
		t.Fatal(err)
	}
	tx.record = &domain.IdempotencyRecord{
		Key:         command.IdempotencyKey,
		RequestHash: hashCommand(command),
		TransferID:  original.ID,
	}
	tx.existingTransfer = original

	result, err := testService(tx).CreateTransfer(context.Background(), command)
	if err != nil {
		t.Fatalf("create duplicate transfer: %v", err)
	}
	if !result.Replayed {
		t.Fatal("duplicate result was not marked replayed")
	}
	if result.Transfer.ID != original.ID {
		t.Fatalf("transfer ID = %q, want %q", result.Transfer.ID, original.ID)
	}
	if len(tx.entries) != 0 {
		t.Fatal("duplicate request created ledger entries")
	}
}

func TestCreateTransferRejectsIdempotencyKeyPayloadMismatch(t *testing.T) {
	tx := newFakeTransaction(false)
	tx.record = &domain.IdempotencyRecord{
		Key:         "key-1",
		RequestHash: "a-different-request-hash",
		TransferID:  "original-transfer",
	}
	tx.existingTransfer = domain.NewPendingTransfer(
		"original-transfer", "key-1", "wallet-1", "wallet-2", 50, time.Now(),
	)

	_, err := testService(tx).CreateTransfer(context.Background(), TransferCommand{
		IdempotencyKey: "key-1",
		FromWalletID:   "wallet-1",
		ToWalletID:     "wallet-2",
		Amount:         100,
	})
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestCreateTransferValidatesCommandBeforeOpeningTransaction(t *testing.T) {
	unitOfWork := &fakeUnitOfWork{tx: newFakeTransaction(true)}
	service := NewTransferServiceWithDependencies(unitOfWork, sequenceIDs(), fixedClock)

	_, err := service.CreateTransfer(context.Background(), TransferCommand{Amount: -1})
	if !errors.Is(err, domain.ErrInvalidIdempotencyKey) {
		t.Fatalf("error = %v, want ErrInvalidIdempotencyKey", err)
	}
	if unitOfWork.called {
		t.Fatal("transaction opened for an invalid command")
	}
}

func testService(tx *fakeTransaction) *TransferService {
	return NewTransferServiceWithDependencies(
		&fakeUnitOfWork{tx: tx},
		sequenceIDs(),
		fixedClock,
	)
}

func sequenceIDs() IDGenerator {
	next := 0
	return func() (string, error) {
		next++
		return fmt.Sprintf("id-%d", next), nil
	}
}

func fixedClock() time.Time {
	return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
}

type fakeUnitOfWork struct {
	tx     *fakeTransaction
	called bool
}

func (u *fakeUnitOfWork) WithinTransaction(
	_ context.Context,
	fn func(repository.Transaction) error,
) error {
	u.called = true
	return fn(u.tx)
}

type fakeTransaction struct {
	claimResult      bool
	record           *domain.IdempotencyRecord
	existingTransfer *domain.Transfer
	createdTransfer  *domain.Transfer
	wallets          map[string]domain.Wallet
	entries          []domain.LedgerEntry
}

func newFakeTransaction(claimResult bool) *fakeTransaction {
	return &fakeTransaction{
		claimResult: claimResult,
		wallets:     make(map[string]domain.Wallet),
	}
}

func (t *fakeTransaction) TryCreateIdempotency(context.Context, string, string) (bool, error) {
	return t.claimResult, nil
}

func (t *fakeTransaction) FindTransferByIdempotencyKey(
	context.Context,
	string,
) (*domain.IdempotencyRecord, *domain.Transfer, error) {
	return t.record, t.existingTransfer, nil
}

func (t *fakeTransaction) LinkIdempotencyToTransfer(context.Context, string, string) error {
	return nil
}

func (t *fakeTransaction) CompleteIdempotency(context.Context, string) error {
	return nil
}

func (t *fakeTransaction) CreateTransfer(_ context.Context, transfer *domain.Transfer) error {
	t.createdTransfer = transfer
	return nil
}

func (t *fakeTransaction) UpdateTransfer(_ context.Context, transfer *domain.Transfer) error {
	t.createdTransfer = transfer
	return nil
}

func (t *fakeTransaction) LockWallets(
	_ context.Context,
	walletIDs []string,
) ([]domain.Wallet, error) {
	wallets := make([]domain.Wallet, 0, len(walletIDs))
	for _, id := range walletIDs {
		if wallet, ok := t.wallets[id]; ok {
			wallets = append(wallets, wallet)
		}
	}
	return wallets, nil
}

func (t *fakeTransaction) UpdateWalletBalance(
	_ context.Context,
	walletID string,
	balance int64,
) error {
	wallet := t.wallets[walletID]
	wallet.Balance = balance
	t.wallets[walletID] = wallet
	return nil
}

func (t *fakeTransaction) CreateLedgerEntries(
	_ context.Context,
	entries []domain.LedgerEntry,
) error {
	t.entries = append(t.entries, entries...)
	return nil
}

package transfer

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/amantiwari/wallet-transfer/internal/app/wallet"
	"github.com/amantiwari/wallet-transfer/internal/domain"
	"github.com/amantiwari/wallet-transfer/internal/testutil/memstore"
)

type frozenClock struct{ t time.Time }

func (c frozenClock) Now() time.Time { return c.t }

type sequenceIDs struct{ n int }

func (s *sequenceIDs) NewID() string {
	s.n++
	return "id-" + itoa(s.n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func newFixture(t *testing.T) (*Service, *wallet.Service, *memstore.Store) {
	t.Helper()
	store := memstore.New()
	clock := frozenClock{t: time.Date(2026, 10, 1, 15, 0, 0, 0, time.UTC)}
	ids := &sequenceIDs{}
	log := slog.New(slog.DiscardHandler)
	wallets := wallet.NewService(store, store, clock, log)
	transfers := NewService(store, store.TransferReader(), store, clock, ids, log)
	return transfers, wallets, store
}

func mustWallet(t *testing.T, svc *wallet.Service, id string, balance int64) {
	t.Helper()
	if _, err := svc.Create(context.Background(), wallet.CreateCommand{ID: id, InitialBalance: balance}); err != nil {
		t.Fatal(err)
	}
}

func TestExecuteMovesFundsAndBalancesLedger(t *testing.T) {
	svc, wallets, store := newFixture(t)
	mustWallet(t, wallets, "wallet_1", 500)
	mustWallet(t, wallets, "wallet_2", 20)

	result, err := svc.Execute(context.Background(), Command{
		IdempotencyKey: "abc123",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Replayed || result.Transfer.Status != domain.StatusProcessed {
		t.Fatalf("result = %+v", result)
	}

	from, err := store.GetByID(context.Background(), "wallet_1")
	if err != nil {
		t.Fatal(err)
	}
	to, err := store.GetByID(context.Background(), "wallet_2")
	if err != nil {
		t.Fatal(err)
	}
	if from.Balance != 400 || to.Balance != 120 {
		t.Fatalf("balances from=%d to=%d", from.Balance, to.Balance)
	}
	entries, err := svc.Ledger(context.Background(), result.Transfer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Type != domain.EntryDebit || entries[1].Type != domain.EntryCredit {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[0].Amount != 100 || entries[1].Amount != 100 {
		t.Fatalf("unbalanced entries %+v", entries)
	}
	if entries[0].WalletID != "wallet_1" || entries[1].WalletID != "wallet_2" {
		t.Fatalf("entry wallets %+v", entries)
	}
}

func TestExecuteReplayDoesNotMoveMoneyAgain(t *testing.T) {
	svc, wallets, store := newFixture(t)
	mustWallet(t, wallets, "wallet_1", 100)
	mustWallet(t, wallets, "wallet_2", 0)
	cmd := Command{IdempotencyKey: "abc123", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 40}

	first, err := svc.Execute(context.Background(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Execute(context.Background(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Replayed || second.Transfer.ID != first.Transfer.ID || second.Transfer.Status != domain.StatusProcessed {
		t.Fatalf("replay = %+v", second)
	}
	from, err := store.GetByID(context.Background(), "wallet_1")
	if err != nil {
		t.Fatal(err)
	}
	if from.Balance != 60 {
		t.Fatalf("balance after replay = %d", from.Balance)
	}
}

func TestExecuteSameKeyDifferentPayloadConflicts(t *testing.T) {
	svc, wallets, store := newFixture(t)
	mustWallet(t, wallets, "wallet_1", 100)
	mustWallet(t, wallets, "wallet_2", 0)
	mustWallet(t, wallets, "wallet_3", 0)
	if _, err := svc.Execute(context.Background(), Command{
		IdempotencyKey: "abc123", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 10,
	}); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Execute(context.Background(), Command{
		IdempotencyKey: "abc123", FromWalletID: "wallet_1", ToWalletID: "wallet_3", Amount: 10,
	})
	if !errors.Is(err, domain.ErrIdempotencyMismatch) {
		t.Fatalf("err = %v", err)
	}
	from, err := store.GetByID(context.Background(), "wallet_1")
	if err != nil {
		t.Fatal(err)
	}
	if from.Balance != 90 {
		t.Fatalf("balance = %d", from.Balance)
	}
}

func TestExecuteInsufficientFundsIsDurableAndIdempotent(t *testing.T) {
	svc, wallets, store := newFixture(t)
	mustWallet(t, wallets, "wallet_1", 30)
	mustWallet(t, wallets, "wallet_2", 0)
	cmd := Command{IdempotencyKey: "abc123", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 50}

	first, err := svc.Execute(context.Background(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	if first.Transfer.Status != domain.StatusFailed || first.Transfer.FailureReason != domain.ReasonInsufficientFunds {
		t.Fatalf("first = %+v", first.Transfer)
	}
	entries, err := svc.Ledger(context.Background(), first.Transfer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed transfer ledger = %+v", entries)
	}
	second, err := svc.Execute(context.Background(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Replayed || second.Transfer.ID != first.Transfer.ID || second.Transfer.Status != domain.StatusFailed {
		t.Fatalf("replay = %+v", second)
	}
	from, err := store.GetByID(context.Background(), "wallet_1")
	if err != nil {
		t.Fatal(err)
	}
	if from.Balance != 30 {
		t.Fatalf("balance = %d", from.Balance)
	}
}

func TestExecuteMissingWalletDoesNotConsumeKey(t *testing.T) {
	svc, wallets, _ := newFixture(t)
	mustWallet(t, wallets, "wallet_2", 0)
	cmd := Command{IdempotencyKey: "abc123", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 5}
	_, err := svc.Execute(context.Background(), cmd)
	if !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("err = %v", err)
	}
	mustWallet(t, wallets, "wallet_1", 20)
	result, err := svc.Execute(context.Background(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	if result.Replayed || result.Transfer.Status != domain.StatusProcessed {
		t.Fatalf("result = %+v", result)
	}
}

func TestExecuteValidationRollsNothingBackBecauseNothingStarted(t *testing.T) {
	svc, wallets, store := newFixture(t)
	mustWallet(t, wallets, "wallet_1", 10)
	tests := []struct {
		name string
		cmd  Command
	}{
		{name: "same wallet", cmd: Command{IdempotencyKey: "k", FromWalletID: "wallet_1", ToWalletID: "wallet_1", Amount: 1}},
		{name: "zero", cmd: Command{IdempotencyKey: "k", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 0}},
		{name: "bad key", cmd: Command{IdempotencyKey: "bad key", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Execute(context.Background(), tt.cmd)
			var validation *domain.ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("err = %v", err)
			}
		})
	}
	from, err := store.GetByID(context.Background(), "wallet_1")
	if err != nil {
		t.Fatal(err)
	}
	if from.Balance != 10 {
		t.Fatalf("balance = %d", from.Balance)
	}
	if _, err := svc.Get(context.Background(), "missing"); !errors.Is(err, domain.ErrTransferNotFound) {
		t.Fatalf("get missing = %v", err)
	}
}

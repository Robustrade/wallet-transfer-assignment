package domain

import (
	"errors"
	"testing"
	"time"
)

func TestNewWallet(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		id      string
		balance int64
		wantErr bool
	}{
		{name: "valid zero", id: "wallet_1", balance: 0},
		{name: "valid funded", id: "wallet-2", balance: 100},
		{name: "empty id", id: "", balance: 0, wantErr: true},
		{name: "bad characters", id: "wallet 1", balance: 0, wantErr: true},
		{name: "negative", id: "wallet_1", balance: -1, wantErr: true},
		{name: "too large", id: "wallet_1", balance: MaxAmount + 1, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wallet, err := NewWallet(tt.id, tt.balance, now)
			if tt.wantErr {
				var validation *ValidationError
				if !errors.As(err, &validation) {
					t.Fatalf("expected validation error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if wallet.Balance != tt.balance || wallet.InitialBalance != tt.balance {
				t.Fatalf("balance = %d initial = %d", wallet.Balance, wallet.InitialBalance)
			}
		})
	}
}

func TestNewPendingTransfer(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	transfer, err := NewPendingTransfer("t1", "abc123", "wallet_1", "wallet_2", 100, now)
	if err != nil {
		t.Fatal(err)
	}
	if transfer.Status != StatusPending {
		t.Fatalf("status = %s", transfer.Status)
	}

	tests := []struct {
		name  string
		id    string
		key   string
		from  string
		to    string
		amt   int64
		field string
	}{
		{name: "same wallet", id: "t1", key: "abc", from: "wallet_1", to: "wallet_1", amt: 10, field: "toWalletId"},
		{name: "zero amount", id: "t1", key: "abc", from: "wallet_1", to: "wallet_2", amt: 0, field: "amount"},
		{name: "negative amount", id: "t1", key: "abc", from: "wallet_1", to: "wallet_2", amt: -5, field: "amount"},
		{name: "bad key", id: "t1", key: "has space", from: "wallet_1", to: "wallet_2", amt: 5, field: "idempotencyKey"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewPendingTransfer(tt.id, tt.key, tt.from, tt.to, tt.amt, now)
			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("expected validation error, got %v", err)
			}
			found := false
			for _, detail := range validation.Details {
				if detail.Field == tt.field {
					found = true
				}
			}
			if !found {
				t.Fatalf("field %s not in %+v", tt.field, validation.Details)
			}
		})
	}
}

func TestTransferTransitions(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	later := now.Add(time.Second)
	pending, err := NewPendingTransfer("t1", "abc", "wallet_1", "wallet_2", 10, now)
	if err != nil {
		t.Fatal(err)
	}

	processed, err := pending.MarkProcessed(later)
	if err != nil {
		t.Fatal(err)
	}
	if processed.Status != StatusProcessed || processed.FailureReason != "" {
		t.Fatalf("processed = %+v", processed)
	}
	if _, err := processed.MarkProcessed(later); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("second process = %v", err)
	}
	if _, err := processed.MarkFailed(ReasonInsufficientFunds, later); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("fail processed = %v", err)
	}

	failed, err := pending.MarkFailed(ReasonInsufficientFunds, later)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != StatusFailed || failed.FailureReason != ReasonInsufficientFunds {
		t.Fatalf("failed = %+v", failed)
	}
	if _, err := failed.MarkProcessed(later); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("process failed = %v", err)
	}
	if _, err := pending.MarkFailed("", later); err == nil {
		t.Fatal("expected missing reason to fail")
	}
	if pending.Status != StatusPending {
		t.Fatal("original transfer was mutated")
	}
}

func TestLedgerPair(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	transfer, err := NewPendingTransfer("t1", "abc", "wallet_1", "wallet_2", 40, now)
	if err != nil {
		t.Fatal(err)
	}
	debit, credit, err := LedgerPair(transfer, "d1", "c1", now)
	if err != nil {
		t.Fatal(err)
	}
	if debit.Type != EntryDebit || debit.WalletID != "wallet_1" || debit.Amount != 40 {
		t.Fatalf("debit = %+v", debit)
	}
	if credit.Type != EntryCredit || credit.WalletID != "wallet_2" || credit.Amount != 40 {
		t.Fatalf("credit = %+v", credit)
	}
	if debit.TransferID != credit.TransferID || debit.Amount != credit.Amount {
		t.Fatal("pair is unbalanced")
	}
	failed, err := transfer.MarkFailed(ReasonInsufficientFunds, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := LedgerPair(failed, "d1", "c1", now); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("ledger for failed = %v", err)
	}
}

func TestNotFoundAndConflictMatchSentinels(t *testing.T) {
	notFound := &NotFoundError{Entity: "wallet", ID: "wallet_9"}
	if !errors.Is(notFound, ErrWalletNotFound) || !errors.Is(notFound, ErrNotFound) {
		t.Fatal("wallet not found sentinel")
	}
	if errors.Is(notFound, ErrTransferNotFound) {
		t.Fatal("wallet matched transfer sentinel")
	}
	conflict := &ConflictError{Entity: "wallet", ID: "wallet_1"}
	if !errors.Is(conflict, ErrWalletExists) || !errors.Is(conflict, ErrConflict) {
		t.Fatal("conflict sentinel")
	}
}

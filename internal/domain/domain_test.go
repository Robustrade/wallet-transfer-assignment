package domain

import (
	"errors"
	"math"
	"testing"
)

func TestNewTransfer(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		from    string
		to      string
		amount  Amount
		wantErr error
	}{
		{name: "valid transfer", id: "transfer-1", from: "wallet-1", to: "wallet-2", amount: 100},
		{name: "missing transfer id", from: "wallet-1", to: "wallet-2", amount: 100, wantErr: ErrInvalidTransferID},
		{name: "missing source wallet", id: "transfer-1", to: "wallet-2", amount: 100, wantErr: ErrInvalidWalletID},
		{name: "missing destination wallet", id: "transfer-1", from: "wallet-1", amount: 100, wantErr: ErrInvalidWalletID},
		{name: "same wallet", id: "transfer-1", from: "wallet-1", to: "wallet-1", amount: 100, wantErr: ErrSameWallet},
		{name: "zero amount", id: "transfer-1", from: "wallet-1", to: "wallet-2", amount: 0, wantErr: ErrInvalidAmount},
		{name: "negative amount", id: "transfer-1", from: "wallet-1", to: "wallet-2", amount: -1, wantErr: ErrInvalidAmount},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewTransfer(tt.id, tt.from, tt.to, tt.amount)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NewTransfer() error = %v, want %v", err, tt.wantErr)
			}
			if err == nil && got.Status != TransferPending {
				t.Fatalf("new transfer status = %q, want %q", got.Status, TransferPending)
			}
		})
	}
}

func TestTransferStateTransitions(t *testing.T) {
	transfer, err := NewTransfer("transfer-1", "wallet-1", "wallet-2", 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := transfer.MarkProcessed(); err != nil {
		t.Fatalf("MarkProcessed() error = %v", err)
	}
	if transfer.Status != TransferProcessed {
		t.Fatalf("status = %q, want %q", transfer.Status, TransferProcessed)
	}
	if err := transfer.MarkFailed("INSUFFICIENT_FUNDS"); !errors.Is(err, ErrInvalidTransferTransition) {
		t.Fatalf("transition from processed to failed error = %v, want %v", err, ErrInvalidTransferTransition)
	}

	failed, err := NewTransfer("transfer-2", "wallet-1", "wallet-2", 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := failed.MarkFailed(""); !errors.Is(err, ErrInvalidFailureCode) {
		t.Fatalf("MarkFailed(empty) error = %v, want %v", err, ErrInvalidFailureCode)
	}
	if err := failed.MarkFailed("INSUFFICIENT_FUNDS"); err != nil {
		t.Fatalf("MarkFailed() error = %v", err)
	}
	if failed.Status != TransferFailed || failed.FailureCode != "INSUFFICIENT_FUNDS" {
		t.Fatalf("failed transfer = %+v", failed)
	}
	if err := failed.MarkFailed("OTHER"); !errors.Is(err, ErrInvalidTransferTransition) {
		t.Fatalf("second failure transition error = %v, want %v", err, ErrInvalidTransferTransition)
	}
}

func TestWalletDebitAndCredit(t *testing.T) {
	wallet := Wallet{ID: "wallet-1", Currency: "INR", Balance: 500}
	if err := wallet.Debit(200); err != nil {
		t.Fatalf("Debit() error = %v", err)
	}
	if wallet.Balance != 300 {
		t.Fatalf("balance after debit = %d, want 300", wallet.Balance)
	}
	if err := wallet.Credit(100); err != nil {
		t.Fatalf("Credit() error = %v", err)
	}
	if wallet.Balance != 400 {
		t.Fatalf("balance after credit = %d, want 400", wallet.Balance)
	}
}

func TestWalletRejectsInvalidBalanceChanges(t *testing.T) {
	tests := []struct {
		name   string
		wallet Wallet
		amount Amount
		action func(*Wallet, Amount) error
		want   error
	}{
		{name: "insufficient funds", wallet: Wallet{Balance: 10}, amount: 11, action: (*Wallet).Debit, want: ErrInsufficientFunds},
		{name: "negative debit", wallet: Wallet{Balance: 10}, amount: -1, action: (*Wallet).Debit, want: ErrInvalidAmount},
		{name: "zero credit", wallet: Wallet{}, amount: 0, action: (*Wallet).Credit, want: ErrInvalidAmount},
		{name: "credit overflow", wallet: Wallet{Balance: Amount(math.MaxInt64)}, amount: 1, action: (*Wallet).Credit, want: ErrBalanceOverflow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := tt.wallet.Balance
			if err := tt.action(&tt.wallet, tt.amount); !errors.Is(err, tt.want) {
				t.Fatalf("balance change error = %v, want %v", err, tt.want)
			}
			if tt.wallet.Balance != before {
				t.Fatalf("balance changed on rejected operation: got %d, want %d", tt.wallet.Balance, before)
			}
		})
	}
}

func TestLedgerEntryValidationAndPair(t *testing.T) {
	debit := LedgerEntry{ID: "entry-1", TransferID: "transfer-1", WalletID: "wallet-1", Type: EntryDebit, Amount: 100}
	credit := LedgerEntry{ID: "entry-2", TransferID: "transfer-1", WalletID: "wallet-2", Type: EntryCredit, Amount: 100}
	if err := debit.Validate(); err != nil {
		t.Fatalf("debit validation error = %v", err)
	}
	if err := credit.Validate(); err != nil {
		t.Fatalf("credit validation error = %v", err)
	}
	if err := ValidateLedgerPair(debit, credit); err != nil {
		t.Fatalf("valid ledger pair error = %v", err)
	}

	credit.Amount = 99
	if err := ValidateLedgerPair(debit, credit); !errors.Is(err, ErrUnbalancedLedger) {
		t.Fatalf("unbalanced pair error = %v, want %v", err, ErrUnbalancedLedger)
	}
}

func TestWalletValidation(t *testing.T) {
	tests := []struct {
		name   string
		wallet Wallet
		want   error
	}{
		{name: "valid wallet", wallet: Wallet{ID: "wallet-1", Currency: "INR", Balance: 0}},
		{name: "missing ID", wallet: Wallet{Currency: "INR"}, want: ErrInvalidWalletID},
		{name: "missing currency", wallet: Wallet{ID: "wallet-1"}, want: ErrInvalidCurrency},
		{name: "negative balance", wallet: Wallet{ID: "wallet-1", Currency: "INR", Balance: -1}, want: ErrInvalidBalance},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.wallet.Validate(); !errors.Is(err, tt.want) {
				t.Fatalf("Validate() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestLedgerEntryRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		entry LedgerEntry
	}{
		{name: "missing entry ID", entry: LedgerEntry{TransferID: "transfer-1", WalletID: "wallet-1", Type: EntryDebit, Amount: 10}},
		{name: "missing transfer ID", entry: LedgerEntry{ID: "entry-1", WalletID: "wallet-1", Type: EntryDebit, Amount: 10}},
		{name: "missing wallet ID", entry: LedgerEntry{ID: "entry-1", TransferID: "transfer-1", Type: EntryDebit, Amount: 10}},
		{name: "invalid type", entry: LedgerEntry{ID: "entry-1", TransferID: "transfer-1", WalletID: "wallet-1", Type: "REFUND", Amount: 10}},
		{name: "non-positive amount", entry: LedgerEntry{ID: "entry-1", TransferID: "transfer-1", WalletID: "wallet-1", Type: EntryDebit, Amount: 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.entry.Validate(); !errors.Is(err, ErrInvalidLedgerEntry) {
				t.Fatalf("Validate() error = %v, want %v", err, ErrInvalidLedgerEntry)
			}
		})
	}
}

package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

func validTransferRequest() domain.TransferRequest {
	return domain.TransferRequest{
		IdempotencyKey: "key-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}
}

func newPendingTransfer(t *testing.T) *domain.Transfer {
	t.Helper()
	transfer, err := domain.NewTransfer("transfer-1", validTransferRequest())
	if err != nil {
		t.Fatalf("NewTransfer: unexpected error: %v", err)
	}
	return transfer
}

func TestNewTransfer_ValidRequestStartsPending(t *testing.T) {
	transfer := newPendingTransfer(t)

	if transfer.ID != "transfer-1" {
		t.Errorf("ID = %q, want %q", transfer.ID, "transfer-1")
	}
	if transfer.IdempotencyKey != "key-1" {
		t.Errorf("IdempotencyKey = %q, want %q", transfer.IdempotencyKey, "key-1")
	}
	if transfer.FromWalletID != "wallet_1" || transfer.ToWalletID != "wallet_2" {
		t.Errorf("wallets = %q -> %q, want wallet_1 -> wallet_2", transfer.FromWalletID, transfer.ToWalletID)
	}
	if transfer.Amount != 100 {
		t.Errorf("Amount = %d, want 100", transfer.Amount)
	}
	if transfer.Status != domain.StatusPending {
		t.Errorf("Status = %q, want %q", transfer.Status, domain.StatusPending)
	}
	if transfer.FailureReason != "" {
		t.Errorf("FailureReason = %q, want empty", transfer.FailureReason)
	}
}

func TestNewTransfer_RejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name      string
		modify    func(r *domain.TransferRequest)
		wantField string
	}{
		{"missing idempotency key", func(r *domain.TransferRequest) { r.IdempotencyKey = "" }, "idempotencyKey"},
		{"idempotency key too long", func(r *domain.TransferRequest) { r.IdempotencyKey = strings.Repeat("k", 256) }, "idempotencyKey"},
		{"missing source wallet", func(r *domain.TransferRequest) { r.FromWalletID = "" }, "fromWalletId"},
		{"missing destination wallet", func(r *domain.TransferRequest) { r.ToWalletID = "" }, "toWalletId"},
		{"zero amount", func(r *domain.TransferRequest) { r.Amount = 0 }, "amount"},
		{"negative amount", func(r *domain.TransferRequest) { r.Amount = -5 }, "amount"},
		{"same source and destination", func(r *domain.TransferRequest) { r.ToWalletID = r.FromWalletID }, "toWalletId"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := validTransferRequest()
			tc.modify(&req)

			_, err := domain.NewTransfer("transfer-1", req)

			var validationErr *domain.ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("error = %v, want *domain.ValidationError", err)
			}
			if validationErr.Field != tc.wantField {
				t.Errorf("Field = %q, want %q", validationErr.Field, tc.wantField)
			}
		})
	}
}

func TestTransfer_MarkProcessedFromPending(t *testing.T) {
	transfer := newPendingTransfer(t)

	if err := transfer.MarkProcessed(); err != nil {
		t.Fatalf("MarkProcessed: unexpected error: %v", err)
	}
	if transfer.Status != domain.StatusProcessed {
		t.Errorf("Status = %q, want %q", transfer.Status, domain.StatusProcessed)
	}
}

func TestTransfer_MarkFailedFromPending(t *testing.T) {
	transfer := newPendingTransfer(t)

	if err := transfer.MarkFailed(domain.FailureInsufficientFunds); err != nil {
		t.Fatalf("MarkFailed: unexpected error: %v", err)
	}
	if transfer.Status != domain.StatusFailed {
		t.Errorf("Status = %q, want %q", transfer.Status, domain.StatusFailed)
	}
	if transfer.FailureReason != domain.FailureInsufficientFunds {
		t.Errorf("FailureReason = %q, want %q", transfer.FailureReason, domain.FailureInsufficientFunds)
	}
}

func TestTransfer_MarkFailedRequiresReason(t *testing.T) {
	transfer := newPendingTransfer(t)

	if err := transfer.MarkFailed(""); err == nil {
		t.Fatal("MarkFailed with empty reason: expected error, got nil")
	}
	if transfer.Status != domain.StatusPending {
		t.Errorf("Status = %q, want it to stay %q", transfer.Status, domain.StatusPending)
	}
}

func TestTransfer_FinalStatesCannotChange(t *testing.T) {
	tests := []struct {
		name       string
		toFinal    func(tr *domain.Transfer) error
		transition func(tr *domain.Transfer) error
	}{
		{"processed to processed", (*domain.Transfer).MarkProcessed, (*domain.Transfer).MarkProcessed},
		{"processed to failed", (*domain.Transfer).MarkProcessed, markInsufficientFunds},
		{"failed to processed", markInsufficientFunds, (*domain.Transfer).MarkProcessed},
		{"failed to failed", markInsufficientFunds, markInsufficientFunds},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			transfer := newPendingTransfer(t)
			if err := tc.toFinal(transfer); err != nil {
				t.Fatalf("moving to final state: unexpected error: %v", err)
			}
			statusBefore := transfer.Status

			err := tc.transition(transfer)

			if !errors.Is(err, domain.ErrInvalidStatusTransition) {
				t.Fatalf("error = %v, want ErrInvalidStatusTransition", err)
			}
			if transfer.Status != statusBefore {
				t.Errorf("Status changed to %q, want it to stay %q", transfer.Status, statusBefore)
			}
		})
	}
}

func TestTransfer_LedgerEntriesForProcessedTransfer(t *testing.T) {
	transfer := newPendingTransfer(t)
	if err := transfer.MarkProcessed(); err != nil {
		t.Fatalf("MarkProcessed: unexpected error: %v", err)
	}

	entries, err := transfer.LedgerEntries()
	if err != nil {
		t.Fatalf("LedgerEntries: unexpected error: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("got %d entries, want exactly 2", len(entries))
	}

	debit, credit := entries[0], entries[1]
	if debit.Type != domain.EntryDebit || debit.WalletID != "wallet_1" || debit.Amount != 100 || debit.TransferID != "transfer-1" {
		t.Errorf("debit entry = %+v, want DEBIT 100 from wallet_1 for transfer-1", debit)
	}
	if credit.Type != domain.EntryCredit || credit.WalletID != "wallet_2" || credit.Amount != 100 || credit.TransferID != "transfer-1" {
		t.Errorf("credit entry = %+v, want CREDIT 100 to wallet_2 for transfer-1", credit)
	}
}

func TestTransfer_LedgerEntriesOnlyForProcessedTransfers(t *testing.T) {
	pending := newPendingTransfer(t)

	failed := newPendingTransfer(t)
	if err := failed.MarkFailed(domain.FailureInsufficientFunds); err != nil {
		t.Fatalf("MarkFailed: unexpected error: %v", err)
	}

	for name, transfer := range map[string]*domain.Transfer{"pending": pending, "failed": failed} {
		t.Run(name, func(t *testing.T) {
			if _, err := transfer.LedgerEntries(); !errors.Is(err, domain.ErrTransferNotProcessed) {
				t.Errorf("error = %v, want ErrTransferNotProcessed", err)
			}
		})
	}
}

func markInsufficientFunds(tr *domain.Transfer) error {
	return tr.MarkFailed(domain.FailureInsufficientFunds)
}

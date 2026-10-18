package domain_test

import (
	"errors"
	"strings"
	"testing"

	"wallet-transfer-assignment/internal/feature/wallet/domain"
)

func validTransfer() domain.Transfer {
	return domain.Transfer{
		IdempotencyKey: "key-12345",
		FromWalletID:   1,
		ToWalletID:     2,
		Amount:         100,
		Status:         domain.TransferPending,
	}
}

func TestTransferValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*domain.Transfer)
		wantErr error // nil => any non-nil error is acceptable when wantAny is set
		wantAny bool
	}{
		{name: "valid", mutate: func(*domain.Transfer) {}},
		{name: "zero amount", mutate: func(tr *domain.Transfer) { tr.Amount = 0 }, wantErr: domain.ErrInvalidAmount},
		{name: "negative amount", mutate: func(tr *domain.Transfer) { tr.Amount = -5 }, wantErr: domain.ErrInvalidAmount},
		{name: "zero sender", mutate: func(tr *domain.Transfer) { tr.FromWalletID = 0 }, wantErr: domain.ErrWalletNotFound},
		{name: "negative receiver", mutate: func(tr *domain.Transfer) { tr.ToWalletID = -1 }, wantErr: domain.ErrWalletNotFound},
		{name: "same wallet", mutate: func(tr *domain.Transfer) { tr.ToWalletID = tr.FromWalletID }, wantErr: domain.ErrSameWallet},
		{name: "empty key", mutate: func(tr *domain.Transfer) { tr.IdempotencyKey = "" }, wantAny: true},
		{name: "key too short", mutate: func(tr *domain.Transfer) { tr.IdempotencyKey = "short12" }, wantAny: true},
		{name: "key at min length", mutate: func(tr *domain.Transfer) { tr.IdempotencyKey = "abcdefgh" }},
		{name: "key too long", mutate: func(tr *domain.Transfer) { tr.IdempotencyKey = strings.Repeat("a", 65) }, wantAny: true},
		{name: "key at max length", mutate: func(tr *domain.Transfer) { tr.IdempotencyKey = strings.Repeat("a", 64) }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr := validTransfer()
			tc.mutate(&tr)
			err := tr.Validate()

			switch {
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("got %v, want %v", err, tc.wantErr)
				}
			case tc.wantAny:
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
			default:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestTransferChangeStatus(t *testing.T) {
	tests := []struct {
		name    string
		from    domain.TransferStatus
		to      domain.TransferStatus
		wantErr bool
	}{
		{"pending to processed", domain.TransferPending, domain.TransferProcessed, false},
		{"pending to failed", domain.TransferPending, domain.TransferFailed, false},
		{"pending to pending", domain.TransferPending, domain.TransferPending, true},
		{"processed to failed", domain.TransferProcessed, domain.TransferFailed, true},
		{"processed to processed", domain.TransferProcessed, domain.TransferProcessed, true},
		{"failed to processed", domain.TransferFailed, domain.TransferProcessed, true},
		{"failed to failed", domain.TransferFailed, domain.TransferFailed, true},
		{"pending to unknown", domain.TransferPending, domain.TransferStatus("BOGUS"), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr := domain.Transfer{Status: tc.from}
			err := tr.ChangeStatus(tc.to)

			if tc.wantErr {
				if !errors.Is(err, domain.ErrInvalidTransition) {
					t.Fatalf("got %v, want ErrInvalidTransition", err)
				}
				if tr.Status != tc.from {
					t.Fatalf("status mutated on failed transition: %s", tr.Status)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tr.Status != tc.to {
				t.Fatalf("status = %s, want %s", tr.Status, tc.to)
			}
		})
	}
}

package domain_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/domain"
)

func TestNewPendingTransfer(t *testing.T) {
	transferID := uuid.New()
	fromWalletID := "wallet_1"
	toWalletID := "wallet_2"
	amount := int64(100)

	transfer, err := domain.NewPendingTransfer(transferID, fromWalletID, toWalletID, amount)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if transfer.ID != transferID {
		t.Errorf("Expected transfer ID %s, got %s", transferID, transfer.ID)
	}
	if transfer.FromWalletID != fromWalletID {
		t.Errorf("Expected from wallet ID %s, got %s", fromWalletID, transfer.FromWalletID)
	}
	if transfer.ToWalletID != toWalletID {
		t.Errorf("Expected to wallet ID %s, got %s", toWalletID, transfer.ToWalletID)
	}
	if transfer.Amount != amount {
		t.Errorf("Expected amount %d, got %d", amount, transfer.Amount)
	}
	if transfer.Status != domain.StatusPending {
		t.Errorf("Expected status %s, got %s", domain.StatusPending, transfer.Status)
	}
	if transfer.FailureReason != nil {
		t.Errorf("Expected failure reason to be nil, got %v", transfer.FailureReason)
	}
}

func TestNewPendingTransfer_Validation(t *testing.T) {
	cases := []struct {
		name         string
		fromWalletID string
		toWalletID   string
		amount       int64
		wantErr      error
	}{
		{name: "empty from wallet id", fromWalletID: "", toWalletID: "wallet_2", amount: 100, wantErr: domain.ErrWalletIdRequired},
		{name: "empty to wallet id", fromWalletID: "wallet_1", toWalletID: "", amount: 100, wantErr: domain.ErrWalletIdRequired},
		{name: "same wallet", fromWalletID: "wallet_1", toWalletID: "wallet_1", amount: 100, wantErr: domain.ErrSameWallet},
		{name: "zero amount", fromWalletID: "wallet_1", toWalletID: "wallet_2", amount: 0, wantErr: domain.ErrInvalidAmount},
		{name: "negative amount", fromWalletID: "wallet_1", toWalletID: "wallet_2", amount: -100, wantErr: domain.ErrInvalidAmount},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := domain.NewPendingTransfer(uuid.New(), tc.fromWalletID, tc.toWalletID, tc.amount)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("Expected error %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestTransfer_MarkProcessed(t *testing.T) {
	cases := []struct {
		name        string
		startStatus domain.TransferStatus
		wantErr     error
		wantStatus  domain.TransferStatus
	}{
		{name: "from pending succeeds", startStatus: domain.StatusPending, wantErr: nil, wantStatus: domain.StatusProcessed},
		{name: "from processed", startStatus: domain.StatusProcessed, wantErr: domain.ErrInvalidTransition, wantStatus: domain.StatusProcessed},
		{name: "from failed rejected", startStatus: domain.StatusFailed, wantErr: domain.ErrInvalidTransition, wantStatus: domain.StatusFailed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			domain, _ := domain.NewPendingTransfer(uuid.New(), "wallet_1", "wallet_2", 100)
			domain.Status = tc.startStatus
			err := domain.MarkProcessed()
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("Expected error %v, got %v", tc.wantErr, err)
			}
			if domain.Status != tc.wantStatus {
				t.Errorf("Expected status %v, got %v", tc.wantStatus, domain.Status)
			}
		})
	}
}

func TestTransfer_MarkFailed(t *testing.T) {
	cases := []struct {
		name        string
		startStatus domain.TransferStatus
		reason      string
		wantErr     error
		wantStatus  domain.TransferStatus
		wantReason  string
	}{
		{name: "from pending with reason succeeds", startStatus: domain.StatusPending, reason: "insufficient funds", wantErr: nil, wantStatus: domain.StatusFailed, wantReason: "insufficient funds"},
		{name: "from pending with empty reason defaults", startStatus: domain.StatusPending, reason: "", wantErr: nil, wantStatus: domain.StatusFailed, wantReason: "unspecified"},
		{name: "from processed rejected", startStatus: domain.StatusProcessed, reason: "insufficient funds", wantErr: domain.ErrInvalidTransition, wantStatus: domain.StatusProcessed, wantReason: "insufficient funds"},
		{name: "from failed rejected", startStatus: domain.StatusFailed, reason: "insufficient funds", wantErr: domain.ErrInvalidTransition, wantStatus: domain.StatusFailed, wantReason: "insufficient funds"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			domain, _ := domain.NewPendingTransfer(uuid.New(), "wallet_1", "wallet_2", 100)
			domain.Status = tc.startStatus
			err := domain.MarkFailed(tc.reason)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("Expected error %v, got %v", tc.wantErr, err)
			}
			if domain.Status != tc.wantStatus {
				t.Errorf("Expected status %v, got %v", tc.wantStatus, domain.Status)
			}
			if tc.wantErr != nil {
				if domain.FailureReason != nil {
					t.Errorf("Expected FailureReason to stay nil on rejected transition, got %v", *domain.FailureReason)
				}
			} else if domain.FailureReason == nil || *domain.FailureReason != tc.wantReason {
				t.Errorf("Expected reason %q, got %v", tc.wantReason, domain.FailureReason)
			}
		})
	}
}

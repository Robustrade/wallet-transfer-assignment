package transfer_test

import (
	"testing"

	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/domain/transfer"
)

func TestTransfer_Validate(t *testing.T) {
	tests := []struct {
		name     string
		transfer transfer.Transfer
		wantErr  bool
		errMsg   string
	}{
		{
			name: "valid transfer",
			transfer: transfer.Transfer{
				ID:           "txn_123",
				TxnID:        "idem_123",
				FromWalletID: "wallet_1",
				ToWalletID:   "wallet_2",
				Amount:       100,
			},
			wantErr: false,
		},
		{
			name: "missing id",
			transfer: transfer.Transfer{
				TxnID:        "idem_123",
				FromWalletID: "wallet_1",
				ToWalletID:   "wallet_2",
				Amount:       100,
			},
			wantErr: true,
			errMsg:  "transfer: id is required",
		},
		{
			name: "missing txnId",
			transfer: transfer.Transfer{
				ID:           "txn_123",
				FromWalletID: "wallet_1",
				ToWalletID:   "wallet_2",
				Amount:       100,
			},
			wantErr: true,
			errMsg:  "transfer: txnId (idempotency key) is required",
		},
		{
			name: "missing fromWalletId",
			transfer: transfer.Transfer{
				ID:         "txn_123",
				TxnID:      "idem_123",
				ToWalletID: "wallet_2",
				Amount:     100,
			},
			wantErr: true,
			errMsg:  "transfer: fromWalletId is required",
		},
		{
			name: "missing toWalletId",
			transfer: transfer.Transfer{
				ID:           "txn_123",
				TxnID:        "idem_123",
				FromWalletID: "wallet_1",
				Amount:       100,
			},
			wantErr: true,
			errMsg:  "transfer: toWalletId is required",
		},
		{
			name: "same from and to wallet",
			transfer: transfer.Transfer{
				ID:           "txn_123",
				TxnID:        "idem_123",
				FromWalletID: "wallet_1",
				ToWalletID:   "wallet_1",
				Amount:       100,
			},
			wantErr: true,
			errMsg:  "transfer: fromWalletId and toWalletId must differ",
		},
		{
			name: "negative amount",
			transfer: transfer.Transfer{
				ID:           "txn_123",
				TxnID:        "idem_123",
				FromWalletID: "wallet_1",
				ToWalletID:   "wallet_2",
				Amount:       -50,
			},
			wantErr: true,
			errMsg:  "transfer: amount must be positive",
		},
		{
			name: "zero amount",
			transfer: transfer.Transfer{
				ID:           "txn_123",
				TxnID:        "idem_123",
				FromWalletID: "wallet_1",
				ToWalletID:   "wallet_2",
				Amount:       0,
			},
			wantErr: true,
			errMsg:  "transfer: amount must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.transfer.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && err.Error() != tt.errMsg {
				t.Errorf("Validate() error msg = %v, want %v", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestTransfer_StateTransitions(t *testing.T) {
	t.Run("CanTransitionTo", func(t *testing.T) {
		tr := transfer.Transfer{Status: transfer.StatusPending}
		if !tr.CanTransitionTo(transfer.StatusProcessed) {
			t.Error("PENDING should transition to PROCESSED")
		}
		if !tr.CanTransitionTo(transfer.StatusFailed) {
			t.Error("PENDING should transition to FAILED")
		}
		
		tr.Status = transfer.StatusProcessed
		if tr.CanTransitionTo(transfer.StatusFailed) {
			t.Error("PROCESSED should not transition to FAILED")
		}
	})

	t.Run("MarkProcessed success", func(t *testing.T) {
		tr := transfer.Transfer{Status: transfer.StatusPending}
		err := tr.MarkProcessed()
		if err != nil {
			t.Errorf("MarkProcessed returned error: %v", err)
		}
		if tr.Status != transfer.StatusProcessed {
			t.Errorf("Expected status PROCESSED, got %v", tr.Status)
		}
	})

	t.Run("MarkProcessed failure", func(t *testing.T) {
		tr := transfer.Transfer{Status: transfer.StatusProcessed}
		err := tr.MarkProcessed()
		if err == nil {
			t.Error("MarkProcessed should fail on non-PENDING transfer")
		}
	})

	t.Run("MarkFailed success", func(t *testing.T) {
		tr := transfer.Transfer{Status: transfer.StatusPending}
		err := tr.MarkFailed("insufficient funds")
		if err != nil {
			t.Errorf("MarkFailed returned error: %v", err)
		}
		if tr.Status != transfer.StatusFailed {
			t.Errorf("Expected status FAILED, got %v", tr.Status)
		}
		if tr.FailureReason != "insufficient funds" {
			t.Errorf("Expected reason 'insufficient funds', got %v", tr.FailureReason)
		}
	})

	t.Run("MarkFailed failure", func(t *testing.T) {
		tr := transfer.Transfer{Status: transfer.StatusFailed}
		err := tr.MarkFailed("another error")
		if err == nil {
			t.Error("MarkFailed should fail on non-PENDING transfer")
		}
	})
}

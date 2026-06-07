package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransfer_Validate(t *testing.T) {
	tests := []struct {
		name     string
		transfer Transfer
		wantErr  string
	}{
		{
			name:     "valid transfer",
			transfer: Transfer{IdempotencyKey: "key1", FromWalletID: "w1", ToWalletID: "w2", Amount: 100},
			wantErr:  "",
		},
		{
			name:     "missing idempotency key",
			transfer: Transfer{FromWalletID: "w1", ToWalletID: "w2", Amount: 100},
			wantErr:  "idempotencyKey is required",
		},
		{
			name:     "missing from wallet",
			transfer: Transfer{IdempotencyKey: "key1", ToWalletID: "w2", Amount: 100},
			wantErr:  "fromWalletId is required",
		},
		{
			name:     "missing to wallet",
			transfer: Transfer{IdempotencyKey: "key1", FromWalletID: "w1", Amount: 100},
			wantErr:  "toWalletId is required",
		},
		{
			name:     "same wallet",
			transfer: Transfer{IdempotencyKey: "key1", FromWalletID: "w1", ToWalletID: "w1", Amount: 100},
			wantErr:  "fromWalletId and toWalletId must be different",
		},
		{
			name:     "zero amount",
			transfer: Transfer{IdempotencyKey: "key1", FromWalletID: "w1", ToWalletID: "w2", Amount: 0},
			wantErr:  "amount must be greater than zero",
		},
		{
			name:     "negative amount",
			transfer: Transfer{IdempotencyKey: "key1", FromWalletID: "w1", ToWalletID: "w2", Amount: -50},
			wantErr:  "amount must be greater than zero",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.transfer.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestTransfer_StateTransitions(t *testing.T) {
	t.Run("PENDING can transition to PROCESSED", func(t *testing.T) {
		tr := &Transfer{Status: TransferStatusPending}
		assert.True(t, tr.CanTransitionTo(TransferStatusProcessed))
		err := tr.MarkProcessed()
		require.NoError(t, err)
		assert.Equal(t, TransferStatusProcessed, tr.Status)
	})

	t.Run("PENDING can transition to FAILED", func(t *testing.T) {
		tr := &Transfer{Status: TransferStatusPending}
		assert.True(t, tr.CanTransitionTo(TransferStatusFailed))
		err := tr.MarkFailed("insufficient funds")
		require.NoError(t, err)
		assert.Equal(t, TransferStatusFailed, tr.Status)
		assert.Equal(t, "insufficient funds", tr.FailureReason)
	})

	t.Run("PROCESSED cannot transition", func(t *testing.T) {
		tr := &Transfer{Status: TransferStatusProcessed}
		assert.False(t, tr.CanTransitionTo(TransferStatusFailed))
		assert.False(t, tr.CanTransitionTo(TransferStatusPending))
	})

	t.Run("FAILED cannot transition", func(t *testing.T) {
		tr := &Transfer{Status: TransferStatusFailed}
		assert.False(t, tr.CanTransitionTo(TransferStatusProcessed))
		assert.False(t, tr.CanTransitionTo(TransferStatusPending))
	})
}

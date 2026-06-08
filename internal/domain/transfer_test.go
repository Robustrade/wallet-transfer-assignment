package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateTransferRequestValidate(t *testing.T) {
	valid := CreateTransferRequest{
		IdempotencyKey: "abc123",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}
	require.NoError(t, valid.Validate())

	t.Run("missing idempotency key", func(t *testing.T) {
		req := valid
		req.IdempotencyKey = ""
		assert.ErrorIs(t, req.Validate(), ErrInvalidIdempotencyKey)
	})

	t.Run("missing wallet ids", func(t *testing.T) {
		req := valid
		req.FromWalletID = ""
		assert.ErrorIs(t, req.Validate(), ErrInvalidWalletID)
	})

	t.Run("same wallet", func(t *testing.T) {
		req := valid
		req.ToWalletID = req.FromWalletID
		assert.ErrorIs(t, req.Validate(), ErrSameWallet)
	})

	t.Run("non-positive amount", func(t *testing.T) {
		req := valid
		req.Amount = 0
		assert.ErrorIs(t, req.Validate(), ErrInvalidAmount)
	})
}

func TestTransferStatusTransitions(t *testing.T) {
	assert.True(t, TransferStatusPending.CanTransitionTo(TransferStatusProcessed))
	assert.True(t, TransferStatusPending.CanTransitionTo(TransferStatusFailed))
	assert.False(t, TransferStatusProcessed.CanTransitionTo(TransferStatusFailed))
	assert.False(t, TransferStatusFailed.CanTransitionTo(TransferStatusProcessed))
}

func TestParseTransferStatus(t *testing.T) {
	status, err := ParseTransferStatus("PENDING")
	require.NoError(t, err)
	assert.Equal(t, TransferStatusPending, status)

	_, err = ParseTransferStatus("UNKNOWN")
	assert.ErrorIs(t, err, ErrInvalidTransferStatus)
}

func TestParseLedgerEntryType(t *testing.T) {
	entryType, err := ParseLedgerEntryType("DEBIT")
	require.NoError(t, err)
	assert.Equal(t, LedgerEntryTypeDebit, entryType)

	_, err = ParseLedgerEntryType("INVALID")
	assert.ErrorIs(t, err, ErrInvalidLedgerEntryType)
}

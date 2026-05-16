package unit

import (
	"testing"

	"wallet-transfer/internal/transfer/domain"

	"github.com/stretchr/testify/assert"
)

func TestNewTransfer(t *testing.T) {
	transfer, err := domain.NewTransfer("transfer_1", "wallet_1", "wallet_2", 1000)

	assert.NoError(t, err)
	assert.Equal(t, "transfer_1", transfer.ID)
	assert.Equal(t, "wallet_1", transfer.FromWalletID)
	assert.Equal(t, "wallet_2", transfer.ToWalletID)
	assert.Equal(t, int64(1000), transfer.Amount)
	assert.Equal(t, domain.StatusPending, transfer.Status)
}

func TestNewTransferSelfTransfer(t *testing.T) {
	_, err := domain.NewTransfer("transfer_1", "wallet_1", "wallet_1", 1000)

	assert.Error(t, err)
}

func TestNewTransferInvalidAmount(t *testing.T) {
	_, err := domain.NewTransfer("transfer_1", "wallet_1", "wallet_2", -100)

	assert.Error(t, err)
}

func TestMarkProcessed(t *testing.T) {
	transfer, _ := domain.NewTransfer("transfer_1", "wallet_1", "wallet_2", 1000)

	err := transfer.MarkProcessed()
	assert.NoError(t, err)
	assert.Equal(t, domain.StatusProcessed, transfer.Status)
	assert.True(t, transfer.IsProcessed())
}

func TestMarkFailed(t *testing.T) {
	transfer, _ := domain.NewTransfer("transfer_1", "wallet_1", "wallet_2", 1000)

	err := transfer.MarkFailed("insufficient funds")
	assert.NoError(t, err)
	assert.Equal(t, domain.StatusFailed, transfer.Status)
	assert.True(t, transfer.IsFailed())
	assert.NotNil(t, transfer.ErrorReason)
	assert.Equal(t, "insufficient funds", *transfer.ErrorReason)
}

func TestMarkProcessedTwice(t *testing.T) {
	transfer, _ := domain.NewTransfer("transfer_1", "wallet_1", "wallet_2", 1000)

	// First mark
	err := transfer.MarkProcessed()
	assert.NoError(t, err)

	// Second mark should fail
	err = transfer.MarkProcessed()
	assert.Error(t, err)
}

func TestTransferStateMachine(t *testing.T) {
	transfer, _ := domain.NewTransfer("transfer_1", "wallet_1", "wallet_2", 1000)

	// PENDING -> PROCESSED
	assert.True(t, transfer.IsPending())
	err := transfer.MarkProcessed()
	assert.NoError(t, err)
	assert.True(t, transfer.IsProcessed())

	// PROCESSED -> FAILED should fail
	err = transfer.MarkFailed("test")
	assert.Error(t, err)
}

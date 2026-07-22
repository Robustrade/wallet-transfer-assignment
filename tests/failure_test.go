package tests

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mritunjaysingh22/wallet-transfer-assignment/internal/service"
)

func TestTransferFailsWithNegativeAmount(t *testing.T) {

	cleanupDatabase()

	from := createWallet(t, 1000)
	to := createWallet(t, 1000)

	req := service.TransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         -100,
	}

	resp, err := transferService.CreateTransfer(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, service.ErrInvalidAmount)

	source := getWallet(t, from)
	destination := getWallet(t, to)

	assert.Equal(t, int64(1000), source.Balance)
	assert.Equal(t, int64(1000), destination.Balance)
}

func TestTransferFailsWithInvalidSourceWallet(t *testing.T) {

	cleanupDatabase()

	to := createWallet(t, 1000)

	req := service.TransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   uuid.NewString(),
		ToWalletID:     to,
		Amount:         100,
	}

	resp, err := transferService.CreateTransfer(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestTransferFailsWithInvalidDestinationWallet(t *testing.T) {

	cleanupDatabase()

	from := createWallet(t, 1000)

	req := service.TransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   from,
		ToWalletID:     uuid.NewString(),
		Amount:         100,
	}

	resp, err := transferService.CreateTransfer(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestTransferFailsWithInvalidUUID(t *testing.T) {

	cleanupDatabase()

	req := service.TransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   "invalid-uuid",
		ToWalletID:     "invalid-uuid",
		Amount:         100,
	}

	resp, err := transferService.CreateTransfer(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestTransferFailsWithEmptyIdempotencyKey(t *testing.T) {

	cleanupDatabase()

	from := createWallet(t, 1000)
	to := createWallet(t, 1000)

	req := service.TransferRequest{
		IdempotencyKey: "",
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         100,
	}

	resp, err := transferService.CreateTransfer(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestTransferDoesNotChangeBalancesOnFailure(t *testing.T) {

	cleanupDatabase()

	from := createWallet(t, 100)
	to := createWallet(t, 500)

	req := service.TransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         1000,
	}

	resp, err := transferService.CreateTransfer(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)

	source := getWallet(t, from)
	destination := getWallet(t, to)

	assert.Equal(t, int64(100), source.Balance)
	assert.Equal(t, int64(500), destination.Balance)
}

func TestTransferCanRecoverAfterFailure(t *testing.T) {

	cleanupDatabase()

	from := createWallet(t, 1000)
	to := createWallet(t, 500)

	badReq := service.TransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         5000,
	}

	_, err := transferService.CreateTransfer(ctx, badReq)
	require.Error(t, err)

	goodReq := service.TransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         300,
	}

	resp, err := transferService.CreateTransfer(ctx, goodReq)

	require.NoError(t, err)
	require.NotNil(t, resp)

	source := getWallet(t, from)
	destination := getWallet(t, to)

	assert.Equal(t, int64(700), source.Balance)
	assert.Equal(t, int64(800), destination.Balance)
}

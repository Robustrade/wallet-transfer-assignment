package tests

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mritunjaysingh22/wallet-transfer-assignment/internal/service"
)

func TestCreateTransfer_Success(t *testing.T) {

	cleanupDatabase()

	from := createWallet(t, 1000)
	to := createWallet(t, 500)

	req := service.TransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         300,
	}

	resp, err := transferService.CreateTransfer(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, int64(300), resp.Amount)
	assert.Equal(t, "PROCESSED", resp.Status)
	assert.Equal(t, from, resp.FromWalletID)
	assert.Equal(t, to, resp.ToWalletID)

	source := getWallet(t, from)
	destination := getWallet(t, to)

	assert.Equal(t, int64(700), source.Balance)
	assert.Equal(t, int64(800), destination.Balance)
}

func TestCreateTransfer_InvalidAmount(t *testing.T) {

	cleanupDatabase()

	from := createWallet(t, 1000)
	to := createWallet(t, 500)

	req := service.TransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         0,
	}

	resp, err := transferService.CreateTransfer(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, service.ErrInvalidAmount)

	source := getWallet(t, from)
	destination := getWallet(t, to)

	assert.Equal(t, int64(1000), source.Balance)
	assert.Equal(t, int64(500), destination.Balance)
}

func TestCreateTransfer_SameWallet(t *testing.T) {

	cleanupDatabase()

	walletID := createWallet(t, 1000)

	req := service.TransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   walletID,
		ToWalletID:     walletID,
		Amount:         100,
	}

	resp, err := transferService.CreateTransfer(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, service.ErrSameWallet)

	wallet := getWallet(t, walletID)

	assert.Equal(t, int64(1000), wallet.Balance)
}

func TestCreateTransfer_InsufficientBalance(t *testing.T) {

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
	assert.ErrorIs(t, err, service.ErrInsufficientBalance)

	source := getWallet(t, from)
	destination := getWallet(t, to)

	assert.Equal(t, int64(100), source.Balance)
	assert.Equal(t, int64(500), destination.Balance)
}

func TestCreateTransfer_MultipleTransfers(t *testing.T) {

	cleanupDatabase()

	from := createWallet(t, 1000)
	to := createWallet(t, 0)

	for i := 0; i < 5; i++ {

		req := service.TransferRequest{
			IdempotencyKey: uuid.NewString(),
			FromWalletID:   from,
			ToWalletID:     to,
			Amount:         100,
		}

		_, err := transferService.CreateTransfer(ctx, req)
		require.NoError(t, err)
	}

	source := getWallet(t, from)
	destination := getWallet(t, to)

	assert.Equal(t, int64(500), source.Balance)
	assert.Equal(t, int64(500), destination.Balance)
}

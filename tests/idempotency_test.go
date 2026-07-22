package tests

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mritunjaysingh22/wallet-transfer-assignment/internal/service"
)

func TestCreateTransfer_Idempotency(t *testing.T) {

	cleanupDatabase()

	from := createWallet(t, 1000)
	to := createWallet(t, 500)

	idempotencyKey := uuid.NewString()

	req := service.TransferRequest{
		IdempotencyKey: idempotencyKey,
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         300,
	}

	// First request
	resp1, err := transferService.CreateTransfer(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp1)

	// Retry with same idempotency key
	resp2, err := transferService.CreateTransfer(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp2)

	// Same transfer should be returned
	assert.Equal(t, resp1.TransferID, resp2.TransferID)
	assert.Equal(t, resp1.IdempotencyKey, resp2.IdempotencyKey)
	assert.Equal(t, resp1.Status, resp2.Status)
	assert.Equal(t, resp1.Amount, resp2.Amount)

	// Money transferred only once
	source := getWallet(t, from)
	destination := getWallet(t, to)

	assert.Equal(t, int64(700), source.Balance)
	assert.Equal(t, int64(800), destination.Balance)
}

func TestCreateTransfer_DifferentIdempotencyKeys(t *testing.T) {

	cleanupDatabase()

	from := createWallet(t, 1000)
	to := createWallet(t, 500)

	req1 := service.TransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         100,
	}

	req2 := service.TransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         100,
	}

	resp1, err := transferService.CreateTransfer(ctx, req1)
	require.NoError(t, err)

	resp2, err := transferService.CreateTransfer(ctx, req2)
	require.NoError(t, err)

	// Different transfers should be created
	assert.NotEqual(t, resp1.TransferID, resp2.TransferID)

	source := getWallet(t, from)
	destination := getWallet(t, to)

	assert.Equal(t, int64(800), source.Balance)
	assert.Equal(t, int64(700), destination.Balance)
}

func TestCreateTransfer_IdempotencyAfterSuccess(t *testing.T) {

	cleanupDatabase()

	from := createWallet(t, 5000)
	to := createWallet(t, 1000)

	key := uuid.NewString()

	req := service.TransferRequest{
		IdempotencyKey: key,
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         1000,
	}

	for i := 0; i < 5; i++ {

		resp, err := transferService.CreateTransfer(ctx, req)

		require.NoError(t, err)
		require.NotNil(t, resp)

		assert.Equal(t, "PROCESSED", resp.Status)
		assert.Equal(t, key, resp.IdempotencyKey)
	}

	source := getWallet(t, from)
	destination := getWallet(t, to)

	assert.Equal(t, int64(4000), source.Balance)
	assert.Equal(t, int64(2000), destination.Balance)
}

func TestCreateTransfer_DuplicateRequestReturnsSameTransfer(t *testing.T) {

	cleanupDatabase()

	from := createWallet(t, 2000)
	to := createWallet(t, 1000)

	key := uuid.NewString()

	req := service.TransferRequest{
		IdempotencyKey: key,
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         500,
	}

	first, err := transferService.CreateTransfer(ctx, req)
	require.NoError(t, err)

	second, err := transferService.CreateTransfer(ctx, req)
	require.NoError(t, err)

	assert.Equal(t, first.TransferID, second.TransferID)
	assert.Equal(t, first.Status, second.Status)
	assert.Equal(t, first.Amount, second.Amount)
	assert.Equal(t, first.FromWalletID, second.FromWalletID)
	assert.Equal(t, first.ToWalletID, second.ToWalletID)
}

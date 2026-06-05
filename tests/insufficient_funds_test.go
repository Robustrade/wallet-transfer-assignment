package tests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"wallet-transfer-service/internal/service"
)

func TestInsufficientFunds(
	t *testing.T,
) {

	ctx := context.Background()

	pool := setupDB(t)

	fromWallet :=
		createWallet(ctx, pool, 50)

	toWallet :=
		createWallet(ctx, pool, 0)

	svc := createService(pool)

	transfer, err :=
		svc.CreateTransfer(
			ctx,
			service.CreateTransferRequest{
				IdempotencyKey: "insufficient",
				FromWalletID:   fromWallet,
				ToWalletID:     toWallet,
				Amount:         100,
			},
		)

	require.Error(t, err)

	require.NotNil(t, transfer)

	//verify transfer failed due to insufficient funds
	var state string

	err = pool.QueryRow(
		ctx,
		`
	SELECT state
	FROM transfers
	WHERE id=$1
	`,
		transfer.ID,
	).Scan(
		&state,
	)

	require.NoError(t, err)

	require.Equal(
		t,
		"FAILED",
		state,
	)

}

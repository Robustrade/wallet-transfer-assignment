package tests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"wallet-transfer-service/internal/service"
)

func TestTransferSuccess(
	t *testing.T,
) {

	ctx := context.Background()

	pool := setupDB(t)

	fromWallet :=
		createWallet(ctx, pool, 1000)

	toWallet :=
		createWallet(ctx, pool, 0)

	svc := createService(pool)

	transfer, err :=
		svc.CreateTransfer(
			ctx,
			service.CreateTransferRequest{
				IdempotencyKey: "success-key",
				FromWalletID:   fromWallet,
				ToWalletID:     toWallet,
				Amount:         100,
			},
		)

	require.NoError(t, err)

	require.NotNil(t, transfer)

	//verify balances

	var balance int64

	err = pool.QueryRow(
		ctx,
		`
	SELECT balance
	FROM wallets
	WHERE id=$1
	`,
		fromWallet,
	).Scan(
		&balance,
	)

	require.NoError(t, err)

	require.Equal(
		t,
		int64(900),
		balance,
	)

	err = pool.QueryRow(
		ctx,
		`
	SELECT balance
	FROM wallets
	WHERE id=$1
	`,
		toWallet,
	).Scan(
		&balance,
	)

	require.NoError(t, err)

	require.Equal(
		t,
		int64(100),
		balance,
	)

	//verify ledger
	var ledgerCount int

	err = pool.QueryRow(
		ctx,
		`
	SELECT COUNT(*)
	FROM ledger_entries
	WHERE transfer_id=$1
	`,
		transfer.ID,
	).Scan(
		&ledgerCount,
	)

	require.NoError(t, err)

	require.Equal(
		t,
		2,
		ledgerCount,
	)
}

package tests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"wallet-transfer-service/internal/service"
)

func TestIdempotency(
	t *testing.T,
) {

	ctx := context.Background()

	pool := setupDB(t)

	fromWallet :=
		createWallet(ctx, pool, 1000)

	toWallet :=
		createWallet(ctx, pool, 0)

	svc := createService(pool)

	req := service.CreateTransferRequest{
		IdempotencyKey: "same-key",
		FromWalletID:   fromWallet,
		ToWalletID:     toWallet,
		Amount:         100,
	}

	first, err :=
		svc.CreateTransfer(
			ctx,
			req,
		)

	require.NoError(t, err)

	second, err :=
		svc.CreateTransfer(
			ctx,
			req,
		)

	require.NoError(t, err)

	require.Equal(
		t,
		first.ID,
		second.ID,
	)

	//Verify transfer count
	var transferCount int

	err = pool.QueryRow(
		ctx,
		`
	SELECT COUNT(*)
	FROM transfers
	`,
	).Scan(
		&transferCount,
	)

	require.NoError(t, err)

	require.Equal(
		t,
		1,
		transferCount,
	)

	//Verify ledger count:
	var ledgerCount int

	err = pool.QueryRow(
		ctx,
		`
	SELECT COUNT(*)
	FROM ledger_entries
	`,
	).Scan(
		&ledgerCount,
	)

	require.NoError(t, err)

	require.Equal(
		t,
		2,
		ledgerCount,
	)

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
}

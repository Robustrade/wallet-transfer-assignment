package tests

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"wallet-transfer-service/internal/service"
)

func TestConcurrentIdempotency(
	t *testing.T,
) {

	ctx := context.Background()

	pool := setupDB(t)

	fromWallet :=
		createWallet(ctx, pool, 1000)

	toWallet :=
		createWallet(ctx, pool, 0)

	svc := createService(pool)

	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {

		wg.Add(1)

		go func() {

			defer wg.Done()

			_, _ =
				svc.CreateTransfer(
					context.Background(),
					service.CreateTransferRequest{
						IdempotencyKey: "shared-key",
						FromWalletID:   fromWallet,
						ToWalletID:     toWallet,
						Amount:         100,
					},
				)

		}()
	}

	wg.Wait()

	//Verify transfer count
	var transferCount int

	err := pool.QueryRow(
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

	//Verify ledger
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

	//Verify balance moved once:
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

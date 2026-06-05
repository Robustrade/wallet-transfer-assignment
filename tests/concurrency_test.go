package tests

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"wallet-transfer-service/internal/service"
)

func TestConcurrentTransfers(
	t *testing.T,
) {

	ctx := context.Background()

	pool := setupDB(t)

	fromWallet :=
		createWallet(ctx, pool, 100)

	toWallet :=
		createWallet(ctx, pool, 0)

	svc := createService(pool)

	var success atomic.Int64

	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {

		wg.Add(1)

		go func(i int) {

			defer wg.Done()

			_, err :=
				svc.CreateTransfer(
					context.Background(),
					service.CreateTransferRequest{
						IdempotencyKey: fmt.Sprintf("k-%d", i),
						FromWalletID:   fromWallet,
						ToWalletID:     toWallet,
						Amount:         10,
					},
				)

			if err == nil {
				success.Add(1)
			}

		}(i)
	}

	wg.Wait()

	require.Equal(
		t,
		int64(10),
		success.Load(),
	)

	// Verify source wallet:
	var balance int64

	err := pool.QueryRow(
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
		int64(0),
		balance,
	)

	// Verify destination wallet:
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

	//Verify processed transfers:
	var processed int

	err = pool.QueryRow(
		ctx,
		`
	SELECT COUNT(*)
	FROM transfers
	WHERE state='PROCESSED'
	`,
	).Scan(
		&processed,
	)

	require.NoError(t, err)

	require.Equal(
		t,
		10,
		processed,
	)

	// Verify ledger:
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
		20,
		ledgerCount,
	)
}

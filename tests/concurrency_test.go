package tests

import (
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mritunjaysingh22/wallet-transfer-assignment/internal/service"
)

func TestConcurrentTransfers(t *testing.T) {

	cleanupDatabase()

	from := createWallet(t, 10000)
	to := createWallet(t, 0)

	const (
		workers = 5
		amount  = int64(10)
	)

	var wg sync.WaitGroup
	errs := make(chan error, workers)

	for i := 0; i < workers; i++ {

		wg.Add(1)

		go func() {
			defer wg.Done()

			req := service.TransferRequest{
				IdempotencyKey: uuid.NewString(),
				FromWalletID:   from,
				ToWalletID:     to,
				Amount:         amount,
			}

			_, err := transferService.CreateTransfer(ctx, req)
			errs <- err
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	source := getWallet(t, from)
	dest := getWallet(t, to)

	assert.Equal(t, int64(9950), source.Balance)
	assert.Equal(t, int64(50), dest.Balance)
}

func TestConcurrentReverseTransfers(t *testing.T) {

	cleanupDatabase()

	w1 := createWallet(t, 10000)
	w2 := createWallet(t, 10000)

	const workers = 5

	var wg sync.WaitGroup
	errs := make(chan error, workers)

	for i := 0; i < workers; i++ {

		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			var req service.TransferRequest

			if i%2 == 0 {

				req = service.TransferRequest{
					IdempotencyKey: uuid.NewString(),
					FromWalletID:   w1,
					ToWalletID:     w2,
					Amount:         10,
				}

			} else {

				req = service.TransferRequest{
					IdempotencyKey: uuid.NewString(),
					FromWalletID:   w2,
					ToWalletID:     w1,
					Amount:         10,
				}
			}

			_, err := transferService.CreateTransfer(ctx, req)
			errs <- err

		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	first := getWallet(t, w1)
	second := getWallet(t, w2)

	assert.Equal(t, int64(9990), first.Balance)
	assert.Equal(t, int64(10010), second.Balance)
}

func TestConcurrentIdempotentRequests(t *testing.T) {

	cleanupDatabase()

	from := createWallet(t, 1000)
	to := createWallet(t, 0)

	key := uuid.NewString()

	const workers = 5

	var wg sync.WaitGroup
	errs := make(chan error, workers)

	for i := 0; i < workers; i++ {

		wg.Add(1)

		go func() {

			defer wg.Done()

			req := service.TransferRequest{
				IdempotencyKey: key,
				FromWalletID:   from,
				ToWalletID:     to,
				Amount:         500,
			}

			_, err := transferService.CreateTransfer(ctx, req)
			errs <- err
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	source := getWallet(t, from)
	dest := getWallet(t, to)

	assert.Equal(t, int64(500), source.Balance)
	assert.Equal(t, int64(500), dest.Balance)
}

func TestConcurrentInsufficientBalance(t *testing.T) {

	cleanupDatabase()

	from := createWallet(t, 100)
	to := createWallet(t, 0)

	const workers = 5

	var wg sync.WaitGroup
	success := 0

	var mu sync.Mutex

	for i := 0; i < workers; i++ {

		wg.Add(1)

		go func() {

			defer wg.Done()

			req := service.TransferRequest{
				IdempotencyKey: uuid.NewString(),
				FromWalletID:   from,
				ToWalletID:     to,
				Amount:         100,
			}

			_, err := transferService.CreateTransfer(ctx, req)

			if err == nil {
				mu.Lock()
				success++
				mu.Unlock()
			}

		}()
	}

	wg.Wait()

	assert.Equal(t, 1, success)

	source := getWallet(t, from)
	dest := getWallet(t, to)

	assert.Equal(t, int64(0), source.Balance)
	assert.Equal(t, int64(100), dest.Balance)
}

func TestStressConcurrentTransfers(t *testing.T) {

	cleanupDatabase()

	from := createWallet(t, 50000)
	to := createWallet(t, 0)

	const (
		workers = 5
		amount  = int64(10)
	)

	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {

		wg.Add(1)

		go func() {

			defer wg.Done()

			req := service.TransferRequest{
				IdempotencyKey: uuid.NewString(),
				FromWalletID:   from,
				ToWalletID:     to,
				Amount:         amount,
			}

			_, _ = transferService.CreateTransfer(ctx, req)

		}()
	}

	wg.Wait()

	source := getWallet(t, from)
	dest := getWallet(t, to)

	assert.Equal(t, int64(49950), source.Balance)
	assert.Equal(t, int64(50), dest.Balance)
}

package integration_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GaureshPatil/wallet-transfer-assignment/internal/domain"
	"github.com/GaureshPatil/wallet-transfer-assignment/internal/service"
)

func TestIdempotency_DuplicateRequestReturnsTheOriginalResultWithoutAnotherTransfer(t *testing.T) {
	e := newEnv(t)
	e.wallet(t, "a", 500)
	e.wallet(t, "b", 0)
	ctx := context.Background()

	first, err := e.transfers.CreateTransfer(ctx, transferInput("abc123", "a", "b", 100))
	require.NoError(t, err)
	second, err := e.transfers.CreateTransfer(ctx, transferInput("abc123", "a", "b", 100))
	require.NoError(t, err)

	assert.False(t, first.Replayed)
	assert.True(t, second.Replayed)
	assert.Equal(t, first.Transfer.ID, second.Transfer.ID, "the original transfer is returned")
	assert.True(t, first.Transfer.CreatedAt.Equal(second.Transfer.CreatedAt), "created at: %s vs %s", first.Transfer.CreatedAt, second.Transfer.CreatedAt)
	assert.True(t, first.Transfer.UpdatedAt.Equal(second.Transfer.UpdatedAt), "updated at: %s vs %s", first.Transfer.UpdatedAt, second.Transfer.UpdatedAt)
	assert.Equal(t, domain.TransferProcessed, second.Transfer.Status)
	assert.EqualValues(t, 400, e.balance(t, "a"), "money moves once")
	assert.EqualValues(t, 100, e.balance(t, "b"))
	assert.Equal(t, 1, e.count(t, "transfers"))
	assert.Equal(t, 2, e.count(t, "ledger_entries"))
	e.assertLedgerReconciles(t)
}

func TestIdempotency_SameKeyWithADifferentPayloadIsRejected(t *testing.T) {
	e := newEnv(t)
	e.wallet(t, "a", 500)
	e.wallet(t, "b", 0)
	e.wallet(t, "c", 0)
	ctx := context.Background()
	_, err := e.transfers.CreateTransfer(ctx, transferInput("k", "a", "b", 100))
	require.NoError(t, err)

	for name, in := range map[string]service.CreateTransferInput{
		"different amount":      transferInput("k", "a", "b", 101),
		"different destination": transferInput("k", "a", "c", 100),
		"different source":      transferInput("k", "b", "a", 100),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := e.transfers.CreateTransfer(ctx, in)
			require.ErrorIs(t, err, domain.ErrIdempotencyKeyReused)
		})
	}
	assert.EqualValues(t, 400, e.balance(t, "a"))
	assert.Equal(t, 1, e.count(t, "transfers"))
}

func TestIdempotency_FailedTransferIsReplayedNotRetried(t *testing.T) {
	e := newEnv(t)
	e.wallet(t, "a", 50)
	e.wallet(t, "b", 0)
	ctx := context.Background()

	first, err := e.transfers.CreateTransfer(ctx, transferInput("k", "a", "b", 100))
	require.NoError(t, err)
	require.Equal(t, domain.TransferFailed, first.Transfer.Status)

	// The wallet is topped up, but a retry with the same key must still
	// return the original outcome: the key identifies one decision.
	e.wallet(t, "rich", 1000)
	_, err = e.transfers.CreateTransfer(ctx, transferInput("topup", "rich", "a", 500))
	require.NoError(t, err)

	replay, err := e.transfers.CreateTransfer(ctx, transferInput("k", "a", "b", 100))
	require.NoError(t, err)
	assert.True(t, replay.Replayed)
	assert.Equal(t, first.Transfer.ID, replay.Transfer.ID)
	assert.Equal(t, domain.TransferFailed, replay.Transfer.Status)
	assert.Equal(t, domain.FailureInsufficientFunds, replay.Transfer.FailureReason)
	assert.EqualValues(t, 550, e.balance(t, "a"))

	fresh, err := e.transfers.CreateTransfer(ctx, transferInput("k-new", "a", "b", 100))
	require.NoError(t, err)
	assert.Equal(t, domain.TransferProcessed, fresh.Transfer.Status, "a new key is a new attempt")
	e.assertLedgerReconciles(t)
}

func TestIdempotency_PartialFailureReleasesTheKeyForARetry(t *testing.T) {
	healthy := newEnv(t)
	healthy.wallet(t, "a", 100)
	healthy.wallet(t, "b", 0)
	broken := newEnvWithStore(healthy.pool, failingLedgerStore{healthy.store})
	ctx := context.Background()

	_, err := broken.transfers.CreateTransfer(ctx, transferInput("k", "a", "b", 40))
	require.Error(t, err)
	assert.Equal(t, 0, healthy.count(t, "idempotency_records"), "the claim is rolled back with the transaction")

	retry, err := healthy.transfers.CreateTransfer(ctx, transferInput("k", "a", "b", 40))
	require.NoError(t, err)
	assert.False(t, retry.Replayed)
	assert.Equal(t, domain.TransferProcessed, retry.Transfer.Status)
	assert.EqualValues(t, 60, healthy.balance(t, "a"))
	assert.Equal(t, 1, healthy.count(t, "transfers"))
	healthy.assertLedgerReconciles(t)
}

func TestIdempotency_UnknownWalletDoesNotBurnTheKey(t *testing.T) {
	e := newEnv(t)
	e.wallet(t, "a", 100)
	ctx := context.Background()

	_, err := e.transfers.CreateTransfer(ctx, transferInput("k", "a", "late", 10))
	require.ErrorIs(t, err, domain.ErrWalletNotFound)

	e.wallet(t, "late", 0)
	res, err := e.transfers.CreateTransfer(ctx, transferInput("k", "a", "late", 10))

	require.NoError(t, err)
	assert.Equal(t, domain.TransferProcessed, res.Transfer.Status)
}

func TestConcurrent_DuplicateRequestsExecuteExactlyOnce(t *testing.T) {
	e := newEnv(t)
	e.wallet(t, "a", 1000)
	e.wallet(t, "b", 0)

	const attempts = 30
	results := make([]*service.TransferResult, attempts)
	errs := make([]error, attempts)
	runConcurrently(t, attempts, time.Minute, func(ctx context.Context, i int) {
		results[i], errs[i] = e.transfers.CreateTransfer(ctx, transferInput("same-key", "a", "b", 100))
	})

	ids := map[uuid.UUID]bool{}
	replays := 0
	for i := range results {
		require.NoError(t, errs[i])
		ids[results[i].Transfer.ID] = true
		assert.Equal(t, domain.TransferProcessed, results[i].Transfer.Status)
		if results[i].Replayed {
			replays++
		}
	}
	assert.Len(t, ids, 1, "every caller sees the same transfer")
	assert.Equal(t, attempts-1, replays, "exactly one request executed, the rest replayed")
	assert.EqualValues(t, 900, e.balance(t, "a"))
	assert.EqualValues(t, 100, e.balance(t, "b"))
	assert.Equal(t, 1, e.count(t, "transfers"))
	assert.Equal(t, 2, e.count(t, "ledger_entries"))
	e.assertLedgerReconciles(t)
}

func TestConcurrent_RetriesAcrossManyKeysExecuteEachKeyOnce(t *testing.T) {
	e := newEnv(t)
	e.wallet(t, "a", 10000)
	e.wallet(t, "b", 0)

	const keys, copies = 8, 6
	var mu sync.Mutex
	processed := map[string]uuid.UUID{}
	runConcurrently(t, keys*copies, 2*time.Minute, func(ctx context.Context, i int) {
		key := fmt.Sprintf("key-%d", i%keys)
		res, err := e.transfers.CreateTransfer(ctx, transferInput(key, "a", "b", 10))
		if !assert.NoError(t, err) { // require must not be used off the test goroutine
			return
		}

		mu.Lock()
		defer mu.Unlock()
		if prev, ok := processed[key]; ok {
			assert.Equal(t, prev, res.Transfer.ID)
		}
		processed[key] = res.Transfer.ID
	})

	assert.Len(t, processed, keys)
	assert.Equal(t, keys, e.count(t, "transfers"))
	assert.EqualValues(t, 10000-keys*10, e.balance(t, "a"))
	e.assertLedgerReconciles(t)
}

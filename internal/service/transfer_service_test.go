package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/robustrade/wallet-transfer-assignment/internal/domain"
)

func newTestService(wallets ...*domain.Wallet) (
	*TransferService, *fakeWalletRepo, *fakeTransferRepo, *fakeLedgerRepo, *fakeIdempotencyRepo,
) {
	w := newFakeWalletRepo(wallets...)
	tr := newFakeTransferRepo()
	l := newFakeLedgerRepo()
	idem := newFakeIdempotencyRepo()
	svc := NewTransferService(fakeTxManager{}, w, tr, l, idem)
	return svc, w, tr, l, idem
}

func TestCreateTransfer_Success(t *testing.T) {
	ctx := context.Background()
	svc, wallets, _, ledger, _ := newTestService(
		&domain.Wallet{ID: "wallet_1", Balance: 500, Currency: "USD"},
		&domain.Wallet{ID: "wallet_2", Balance: 200, Currency: "USD"},
	)

	result, err := svc.CreateTransfer(ctx, CreateTransferInput{
		IdempotencyKey: "key-1", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100,
	})
	require.NoError(t, err)
	assert.Equal(t, domain.TransferProcessed, result.Status)
	assert.False(t, result.Replayed)

	from, err := wallets.Get(ctx, "wallet_1")
	require.NoError(t, err)
	to, err := wallets.Get(ctx, "wallet_2")
	require.NoError(t, err)
	assert.EqualValues(t, 400, from.Balance)
	assert.EqualValues(t, 300, to.Balance)

	entries, err := ledger.ListByWallet(ctx, "wallet_1")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, domain.EntryDebit, entries[0].EntryType)
	assert.EqualValues(t, 100, entries[0].Amount)

	entries, err = ledger.ListByWallet(ctx, "wallet_2")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, domain.EntryCredit, entries[0].EntryType)
}

func TestCreateTransfer_InsufficientFunds(t *testing.T) {
	ctx := context.Background()
	svc, wallets, _, ledger, _ := newTestService(
		&domain.Wallet{ID: "wallet_1", Balance: 50, Currency: "USD"},
		&domain.Wallet{ID: "wallet_2", Balance: 0, Currency: "USD"},
	)

	result, err := svc.CreateTransfer(ctx, CreateTransferInput{
		IdempotencyKey: "key-1", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100,
	})
	require.NoError(t, err)
	assert.Equal(t, domain.TransferFailed, result.Status)
	assert.NotEmpty(t, result.FailureReason)

	from, _ := wallets.Get(ctx, "wallet_1")
	to, _ := wallets.Get(ctx, "wallet_2")
	assert.EqualValues(t, 50, from.Balance, "balance must be untouched on failure")
	assert.EqualValues(t, 0, to.Balance)

	entries, _ := ledger.ListByWallet(ctx, "wallet_1")
	assert.Empty(t, entries, "a FAILED transfer must not produce ledger entries")
}

func TestCreateTransfer_IdempotentReplay(t *testing.T) {
	ctx := context.Background()
	svc, wallets, _, ledger, _ := newTestService(
		&domain.Wallet{ID: "wallet_1", Balance: 500, Currency: "USD"},
		&domain.Wallet{ID: "wallet_2", Balance: 200, Currency: "USD"},
	)

	in := CreateTransferInput{IdempotencyKey: "dup-key", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100}

	first, err := svc.CreateTransfer(ctx, in)
	require.NoError(t, err)
	assert.False(t, first.Replayed)

	second, err := svc.CreateTransfer(ctx, in)
	require.NoError(t, err)
	assert.True(t, second.Replayed)
	assert.Equal(t, first.TransferID, second.TransferID)

	from, _ := wallets.Get(ctx, "wallet_1")
	assert.EqualValues(t, 400, from.Balance, "replay must not move money twice")

	entries, _ := ledger.ListByWallet(ctx, "wallet_1")
	assert.Len(t, entries, 1, "replay must not duplicate ledger entries")
}

func TestCreateTransfer_IdempotencyKeyReusedWithDifferentPayload(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _, _ := newTestService(
		&domain.Wallet{ID: "wallet_1", Balance: 500, Currency: "USD"},
		&domain.Wallet{ID: "wallet_2", Balance: 200, Currency: "USD"},
	)

	_, err := svc.CreateTransfer(ctx, CreateTransferInput{
		IdempotencyKey: "shared-key", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100,
	})
	require.NoError(t, err)

	_, err = svc.CreateTransfer(ctx, CreateTransferInput{
		IdempotencyKey: "shared-key", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 999,
	})
	assert.ErrorIs(t, err, domain.ErrIdempotencyKeyReused)
}

func TestCreateTransfer_RequestInProgress(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _, idem := newTestService(
		&domain.Wallet{ID: "wallet_1", Balance: 500, Currency: "USD"},
		&domain.Wallet{ID: "wallet_2", Balance: 200, Currency: "USD"},
	)

	in := CreateTransferInput{IdempotencyKey: "in-flight", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100}

	// Simulate another request that has already claimed the key (same
	// payload hash) but has not finished processing yet.
	_, claimed, err := idem.Claim(ctx, in.IdempotencyKey, requestHash(in))
	require.NoError(t, err)
	require.True(t, claimed)

	_, err = svc.CreateTransfer(ctx, in)
	assert.ErrorIs(t, err, domain.ErrRequestInProgress)
}

func TestCreateTransfer_WalletNotFoundReleasesKeyForRetry(t *testing.T) {
	ctx := context.Background()
	svc, wallets, _, _, _ := newTestService(
		&domain.Wallet{ID: "wallet_1", Balance: 500, Currency: "USD"},
	)

	in := CreateTransferInput{IdempotencyKey: "retryable", FromWalletID: "wallet_1", ToWalletID: "wallet_missing", Amount: 100}

	_, err := svc.CreateTransfer(ctx, in)
	assert.ErrorIs(t, err, domain.ErrWalletNotFound)

	// Now the wallet shows up (e.g. provisioned afterwards) and the same key retries.
	require.NoError(t, wallets.Create(ctx, &domain.Wallet{ID: "wallet_missing", Balance: 0, Currency: "USD"}))

	result, err := svc.CreateTransfer(ctx, in)
	require.NoError(t, err, "a released key must be retryable, not permanently wedged")
	assert.Equal(t, domain.TransferProcessed, result.Status)
}

func TestCreateTransfer_CurrencyMismatchRejected(t *testing.T) {
	ctx := context.Background()
	svc, wallets, _, ledger, _ := newTestService(
		&domain.Wallet{ID: "wallet_usd", Balance: 500, Currency: "USD"},
		&domain.Wallet{ID: "wallet_eur", Balance: 200, Currency: "EUR"},
	)

	_, err := svc.CreateTransfer(ctx, CreateTransferInput{
		IdempotencyKey: "cross-currency", FromWalletID: "wallet_usd", ToWalletID: "wallet_eur", Amount: 100,
	})
	assert.ErrorIs(t, err, domain.ErrCurrencyMismatch)

	from, _ := wallets.Get(ctx, "wallet_usd")
	assert.EqualValues(t, 500, from.Balance, "a rejected cross-currency transfer must not move money")

	entries, _ := ledger.ListByWallet(ctx, "wallet_usd")
	assert.Empty(t, entries)
}

func TestCreateTransfer_ValidationErrors(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _, _ := newTestService(
		&domain.Wallet{ID: "wallet_1", Balance: 500, Currency: "USD"},
		&domain.Wallet{ID: "wallet_2", Balance: 200, Currency: "USD"},
	)

	_, err := svc.CreateTransfer(ctx, CreateTransferInput{
		IdempotencyKey: "k1", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 0,
	})
	assert.ErrorIs(t, err, domain.ErrInvalidAmount)

	_, err = svc.CreateTransfer(ctx, CreateTransferInput{
		IdempotencyKey: "k2", FromWalletID: "wallet_1", ToWalletID: "wallet_1", Amount: 10,
	})
	assert.ErrorIs(t, err, domain.ErrSameWallet)

	_, err = svc.CreateTransfer(ctx, CreateTransferInput{
		IdempotencyKey: "", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 10,
	})
	assert.ErrorIs(t, err, domain.ErrMissingIdempotencyKey)
}

// TestCreateTransfer_AmbiguousCommitDoesNotReleaseKey covers the case where
// the DB transaction's commit itself fails in a way that doesn't prove
// whether it landed (domain.ErrCommitOutcomeUnknown). Releasing the
// idempotency key here would be unsafe: if the commit actually succeeded on
// the server, a released key lets a retry re-run the transfer and move money
// twice, so CreateTransfer must not call Release for this error.
//
// The fake TxManager runs fn to completion against in-memory maps with no
// real rollback semantics, so it can't reproduce "commit failed and nothing
// landed" -- that atomicity guarantee is proven separately against real
// Postgres in the integration tests. What's tested here, at the service
// layer, is the specific decision CreateTransfer makes on this error: leave
// the key alone rather than releasing it.
func TestCreateTransfer_AmbiguousCommitDoesNotReleaseKey(t *testing.T) {
	ctx := context.Background()
	wallets := newFakeWalletRepo(
		&domain.Wallet{ID: "wallet_1", Balance: 500, Currency: "USD"},
		&domain.Wallet{ID: "wallet_2", Balance: 200, Currency: "USD"},
	)
	idem := newFakeIdempotencyRepo()
	commitErr := fmt.Errorf("commit tx: %w: %w", domain.ErrCommitOutcomeUnknown, errors.New("connection reset"))
	svc := NewTransferService(
		fakeTxManager{commitErr: commitErr},
		wallets, newFakeTransferRepo(), newFakeLedgerRepo(), idem,
	)

	in := CreateTransferInput{IdempotencyKey: "ambiguous-key", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100}

	_, err := svc.CreateTransfer(ctx, in)
	require.ErrorIs(t, err, domain.ErrCommitOutcomeUnknown)
	assert.Zero(t, idem.releaseCalls, "an ambiguous commit outcome must not release the idempotency key")
}

// TestCreateTransfer_ConcurrentDuplicates fires the same idempotency key from
// many goroutines at once. Real row-level locking is proven against Postgres
// in the integration test; this test proves the service-level idempotency
// contract holds: money moves exactly once no matter how many duplicates race.
func TestCreateTransfer_ConcurrentDuplicates(t *testing.T) {
	ctx := context.Background()
	svc, wallets, _, _, _ := newTestService(
		&domain.Wallet{ID: "wallet_1", Balance: 1000, Currency: "USD"},
		&domain.Wallet{ID: "wallet_2", Balance: 0, Currency: "USD"},
	)

	in := CreateTransferInput{IdempotencyKey: "race-key", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100}

	const n = 20
	var wg sync.WaitGroup
	results := make([]*TransferResult, n)
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = svc.CreateTransfer(ctx, in)
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			assert.ErrorIs(t, errs[i], domain.ErrRequestInProgress)
		}
	}

	from, _ := wallets.Get(ctx, "wallet_1")
	to, _ := wallets.Get(ctx, "wallet_2")
	assert.EqualValues(t, 900, from.Balance, "money must move exactly once across all duplicate racers")
	assert.EqualValues(t, 100, to.Balance)
}

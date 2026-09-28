package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LokilokeshM/wallet-transfer-assignment/internal/domain"
)

func TestIdempotency_SameKeySameBodyReplays(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, driver string) {
		e := newEnv(t, driver)
		e.seedWallet("a", 1000, "SGD")
		e.seedWallet("b", 0, "SGD")

		first, err := e.transfer("k1", "a", "b", 100)
		require.NoError(t, err)
		second, err := e.transfer("k1", "a", "b", 100)
		require.NoError(t, err)

		assert.False(t, first.Replayed)
		assert.True(t, second.Replayed)
		// The replay is the original result, field for field.
		assert.Equal(t, first.Transfer.ID, second.Transfer.ID)
		assert.Equal(t, first.Transfer.Status, second.Transfer.Status)
		assert.Equal(t, first.Transfer.Amount, second.Transfer.Amount)
		assert.True(t, first.Transfer.CreatedAt.Equal(second.Transfer.CreatedAt),
			"createdAt %v vs %v", first.Transfer.CreatedAt, second.Transfer.CreatedAt)

		assert.Equal(t, int64(900), e.balance("a"), "money moved once")
		assert.Equal(t, int64(1), e.count("transfers"))
		assert.Equal(t, int64(2), e.count("ledger_entries"))
	})
}

func TestIdempotency_FiveSequentialReplays(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, driver string) {
		e := newEnv(t, driver)
		e.seedWallet("a", 1000, "SGD")
		e.seedWallet("b", 0, "SGD")

		first, err := e.transfer("k1", "a", "b", 100)
		require.NoError(t, err)
		for i := 0; i < 5; i++ {
			res, err := e.transfer("k1", "a", "b", 100)
			require.NoError(t, err)
			assert.True(t, res.Replayed)
			assert.Equal(t, first.Transfer.ID, res.Transfer.ID)
		}

		assert.Equal(t, int64(1), e.count("transfers"))
		assert.Equal(t, int64(900), e.balance("a"))
	})
}

func TestIdempotency_SameKeyDifferentPayloadIsRejected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		from, to string
		amount   int64
	}{
		{"different amount", "a", "b", 101},
		{"different from", "c", "b", 100},
		{"different to", "a", "c", 100},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			forEachDriver(t, func(t *testing.T, driver string) {
				e := newEnv(t, driver)
				e.seedWallet("a", 1000, "SGD")
				e.seedWallet("b", 0, "SGD")
				e.seedWallet("c", 1000, "SGD")
				_, err := e.transfer("k1", "a", "b", 100)
				require.NoError(t, err)

				_, err = e.transfer("k1", tc.from, tc.to, tc.amount)

				require.ErrorIs(t, err, domain.ErrIdempotencyKeyReused)
				assert.Equal(t, int64(1), e.count("transfers"), "no new transfer")
				assert.Equal(t, int64(2), e.count("ledger_entries"))
				assert.Equal(t, int64(1), e.count("idempotency_records"))
				assert.Equal(t, int64(900), e.balance("a"))
				assert.Equal(t, int64(1000), e.balance("c"))
			})
		})
	}
}

func TestIdempotency_FailedResultIsReplayedEvenAfterTopUp(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, driver string) {
		e := newEnv(t, driver)
		e.seedWallet("a", 50, "SGD")
		e.seedWallet("b", 0, "SGD")
		e.seedWallet("bank", 1000, "SGD")

		first, err := e.transfer("k1", "a", "b", 100)
		require.NoError(t, err)
		require.Equal(t, domain.StatusFailed, first.Transfer.Status)

		// The source now has enough money, via a real transfer (so the
		// ledger stays consistent).
		_, err = e.transfer("top-up", "bank", "a", 500)
		require.NoError(t, err)
		require.Equal(t, int64(550), e.balance("a"))

		// The retry must return the ORIGINAL outcome, not re-evaluate it.
		retry, err := e.transfer("k1", "a", "b", 100)
		require.NoError(t, err)
		assert.True(t, retry.Replayed)
		assert.Equal(t, first.Transfer.ID, retry.Transfer.ID)
		assert.Equal(t, domain.StatusFailed, retry.Transfer.Status)
		assert.Equal(t, domain.ReasonInsufficientFunds, retry.Transfer.FailureReason)
		assert.Equal(t, int64(550), e.balance("a"), "no money moved by the replay")
		assert.Equal(t, int64(0), e.balance("b"))
	})
}

func TestIdempotency_KeyCanBeReusedAfterWalletNotFound(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, driver string) {
		e := newEnv(t, driver)
		e.seedWallet("a", 100, "SGD")

		_, err := e.transfer("k1", "a", "late", 10)
		require.ErrorIs(t, err, domain.ErrWalletNotFound)

		// Nothing was stored for k1, so once the wallet exists the same
		// request simply succeeds.
		e.seedWallet("late", 0, "SGD")
		res, err := e.transfer("k1", "a", "late", 10)
		require.NoError(t, err)
		assert.False(t, res.Replayed)
		assert.Equal(t, domain.StatusProcessed, res.Transfer.Status)
		assert.Equal(t, int64(10), e.balance("late"))
	})
}

func TestIdempotency_KeysAreCaseSensitive(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, driver string) {
		e := newEnv(t, driver)
		e.seedWallet("a", 100, "SGD")
		e.seedWallet("b", 0, "SGD")

		upper, err := e.transfer("ABC", "a", "b", 10)
		require.NoError(t, err)
		lower, err := e.transfer("abc", "a", "b", 10)
		require.NoError(t, err)

		assert.False(t, lower.Replayed)
		assert.NotEqual(t, upper.Transfer.ID, lower.Transfer.ID)
		assert.Equal(t, int64(2), e.count("transfers"))
		assert.Equal(t, int64(80), e.balance("a"))
	})
}

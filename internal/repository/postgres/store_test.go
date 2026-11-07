package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/celinagupta/wallet-transfer-assignment/internal/domain"
	"github.com/celinagupta/wallet-transfer-assignment/internal/repository"
	"github.com/celinagupta/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/celinagupta/wallet-transfer-assignment/internal/testutil/pgtest"
)

func TestMain(m *testing.M) { pgtest.Main(m) }

func newTransfer(key string) domain.Transfer {
	return domain.NewTransfer(uuid.New(), domain.TransferRequest{
		IdempotencyKey: key, FromWalletID: "a", ToWalletID: "b", Amount: 10,
	})
}

func TestLockWalletsReturnsOnlyExistingWalletsInIDOrder(t *testing.T) {
	pool := pgtest.Pool(t)
	pgtest.SeedWallets(t, pool, map[string]int64{"b": 20, "a": 10})
	store := postgres.NewStore(pool)

	var got []domain.Wallet
	err := store.WithinTx(context.Background(), func(repo repository.Repository) error {
		var err error
		got, err = repo.LockWallets(context.Background(), "b", "missing", "a")
		return err
	})

	require.NoError(t, err)
	assert.Equal(t, []domain.Wallet{{ID: "a", Balance: 10}, {ID: "b", Balance: 20}}, got)
}

func TestInsertTransferReportsTakenIdempotencyKey(t *testing.T) {
	pool := pgtest.Pool(t)
	pgtest.SeedWallets(t, pool, map[string]int64{"a": 100, "b": 0})
	store := postgres.NewStore(pool)
	ctx := context.Background()

	first, second := newTransfer("k1"), newTransfer("k1")
	err := store.WithinTx(ctx, func(repo repository.Repository) error {
		inserted, err := repo.InsertTransfer(ctx, &first)
		require.NoError(t, err)
		assert.True(t, inserted)
		assert.False(t, first.CreatedAt.IsZero(), "created_at is returned")

		inserted, err = repo.InsertTransfer(ctx, &second)
		require.NoError(t, err)
		assert.False(t, inserted, "second insert with the same key is skipped")

		stored, err := repo.TransferByIdempotencyKey(ctx, "k1")
		require.NoError(t, err)
		assert.Equal(t, first.ID, stored.ID, "the original transfer is kept")
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 1, pgtest.Count(t, pool, "transfers"))
}

func TestInsertTransferWithoutKeyNeverConflicts(t *testing.T) {
	pool := pgtest.Pool(t)
	pgtest.SeedWallets(t, pool, map[string]int64{"a": 100, "b": 0})
	store := postgres.NewStore(pool)
	ctx := context.Background()

	err := store.WithinTx(ctx, func(repo repository.Repository) error {
		for range 2 {
			tr := newTransfer("")
			inserted, err := repo.InsertTransfer(ctx, &tr)
			require.NoError(t, err)
			assert.True(t, inserted)
		}
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 2, pgtest.Count(t, pool, "transfers"))
}

func TestUpdateTransferStatusRefusesTerminalTransfer(t *testing.T) {
	pool := pgtest.Pool(t)
	pgtest.SeedWallets(t, pool, map[string]int64{"a": 100, "b": 0})
	store := postgres.NewStore(pool)
	ctx := context.Background()
	tr := newTransfer("k1")

	err := store.WithinTx(ctx, func(repo repository.Repository) error {
		_, err := repo.InsertTransfer(ctx, &tr)
		require.NoError(t, err)
		require.NoError(t, tr.MarkFailed(domain.FailureInsufficientFunds))
		require.NoError(t, repo.UpdateTransferStatus(ctx, tr))

		// A stale copy that still believes it is PENDING tries to process it.
		stale := tr
		stale.Status, stale.FailureReason = domain.StatusProcessed, ""
		return repo.UpdateTransferStatus(ctx, stale)
	})

	require.ErrorIs(t, err, domain.ErrInvalidTransition)
	assert.Equal(t, 0, pgtest.Count(t, pool, "transfers"), "the failed transaction rolled back")
}

func TestWithinTxRollsBackOnError(t *testing.T) {
	pool := pgtest.Pool(t)
	pgtest.SeedWallets(t, pool, map[string]int64{"a": 100, "b": 0})
	store := postgres.NewStore(pool)
	ctx := context.Background()
	boom := errors.New("boom")

	err := store.WithinTx(ctx, func(repo repository.Repository) error {
		require.NoError(t, repo.AddToBalance(ctx, "a", -100))
		return boom
	})

	require.ErrorIs(t, err, boom)
	assert.Equal(t, int64(100), pgtest.Balance(t, pool, "a"))
}

func TestLockTimeoutIsReportedAsRetryable(t *testing.T) {
	pool := pgtest.Pool(t)
	pgtest.SeedWallets(t, pool, map[string]int64{"a": 100})
	ctx := context.Background()

	holding := make(chan struct{})
	release := make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- postgres.NewStore(pool).WithinTx(ctx, func(repo repository.Repository) error {
			if _, err := repo.LockWallets(ctx, "a"); err != nil {
				return err
			}
			close(holding)
			<-release
			return nil
		})
	}()
	<-holding

	err := postgres.NewStore(pool).WithTimeouts(100*time.Millisecond, time.Second).
		WithinTx(ctx, func(repo repository.Repository) error {
			_, err := repo.LockWallets(ctx, "a")
			return err
		})
	close(release)

	require.ErrorIs(t, err, domain.ErrTemporarilyUnavailable)
	require.NoError(t, <-holderDone)
}

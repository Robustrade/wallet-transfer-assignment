package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
	"github.com/Robustrade/wallet-transfer-assignment/internal/testutil/pgtest"
)

func newDB(t *testing.T, opts postgres.Options) (*postgres.DB, *pgxpool.Pool) {
	t.Helper()
	pool := pgtest.NewDB(t)
	_, err := pool.Exec(context.Background(),
		`INSERT INTO wallets (id, balance) VALUES ('wallet_1', 100), ('wallet_2', 0)`)
	require.NoError(t, err)
	return postgres.New(pool, opts), pool
}

func newTransfer(t *testing.T) domain.Transfer {
	t.Helper()
	tr, err := domain.NewPendingTransfer(uuid.New(), domain.TransferRequest{
		IdempotencyKey: "key-" + uuid.NewString(), FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 10,
	}, time.Now().UTC())
	require.NoError(t, err)
	return tr
}

func TestInTx_RollsBackEverythingWhenTheCallbackFails(t *testing.T) {
	db, _ := newDB(t, postgres.Options{})
	ctx := context.Background()
	boom := errors.New("boom")

	err := db.InTx(ctx, func(ctx context.Context, tx service.Store) error {
		require.NoError(t, tx.Wallets().Debit(ctx, "wallet_1", 50))
		return boom
	})

	require.ErrorIs(t, err, boom)
	w, err := db.Wallets().Get(ctx, "wallet_1")
	require.NoError(t, err)
	require.Equal(t, int64(100), w.Balance)
}

func TestUpdateStatus_IsGuardedByTheExpectedCurrentStatus(t *testing.T) {
	db, _ := newDB(t, postgres.Options{})
	ctx := context.Background()
	tr := newTransfer(t)
	require.NoError(t, db.Transfers().Create(ctx, tr))

	require.NoError(t, tr.Process(time.Now().UTC()))
	require.NoError(t, db.Transfers().UpdateStatus(ctx, tr, domain.StatusPending))

	// A second (duplicate or stale) transition from PENDING must not overwrite PROCESSED.
	stale := tr
	stale.Status, stale.FailureReason = domain.StatusFailed, domain.FailureInsufficientFunds
	err := db.Transfers().UpdateStatus(ctx, stale, domain.StatusPending)
	require.ErrorIs(t, err, domain.ErrInvalidTransition)

	stored, err := db.Transfers().Get(ctx, tr.ID)
	require.NoError(t, err)
	require.Equal(t, domain.StatusProcessed, stored.Status)
}

func TestClaim_SecondClaimOfTheSameKeyIsRejected(t *testing.T) {
	db, _ := newDB(t, postgres.Options{})
	ctx := context.Background()
	tr := newTransfer(t)

	err := db.InTx(ctx, func(ctx context.Context, tx service.Store) error {
		claimed, err := tx.Idempotency().Claim(ctx, service.IdempotencyRecord{
			Key: tr.IdempotencyKey, RequestHash: "h1", TransferID: tr.ID,
		})
		require.NoError(t, err)
		require.True(t, claimed)
		return tx.Transfers().Create(ctx, tr)
	})
	require.NoError(t, err)

	claimed, err := db.Idempotency().Claim(ctx, service.IdempotencyRecord{
		Key: tr.IdempotencyKey, RequestHash: "h2", TransferID: uuid.New(),
	})
	require.NoError(t, err)
	require.False(t, claimed)

	rec, err := db.Idempotency().Get(ctx, tr.IdempotencyKey)
	require.NoError(t, err)
	require.Equal(t, "h1", rec.RequestHash, "the original claim is kept")
	require.Equal(t, tr.ID, rec.TransferID)

	stored, err := db.Transfers().Get(ctx, tr.ID)
	require.NoError(t, err)
	require.Equal(t, tr.IdempotencyKey, stored.IdempotencyKey)
}

func TestGetForUpdate_ReturnsErrBusyWhenTheLockWaitExceedsTheTimeout(t *testing.T) {
	db, _ := newDB(t, postgres.Options{LockTimeout: 100 * time.Millisecond})
	ctx := context.Background()

	locked := make(chan struct{})
	release := make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- db.InTx(ctx, func(ctx context.Context, tx service.Store) error {
			if _, err := tx.Wallets().GetForUpdate(ctx, "wallet_1"); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	<-locked

	err := db.InTx(ctx, func(ctx context.Context, tx service.Store) error {
		_, err := tx.Wallets().GetForUpdate(ctx, "wallet_1")
		return err
	})
	close(release)

	require.ErrorIs(t, err, domain.ErrBusy)
	require.NoError(t, <-holderDone)
}

func TestWalletLookups_ReportUnknownWallets(t *testing.T) {
	db, _ := newDB(t, postgres.Options{})
	ctx := context.Background()

	_, err := db.Wallets().Get(ctx, "nope")
	require.ErrorIs(t, err, domain.ErrWalletNotFound)
	require.ErrorIs(t, db.Wallets().Debit(ctx, "nope", 1), domain.ErrWalletNotFound)
	require.ErrorIs(t, db.Wallets().Credit(ctx, "nope", 1), domain.ErrWalletNotFound)
	_, err = db.Transfers().Get(ctx, uuid.New())
	require.ErrorIs(t, err, domain.ErrTransferNotFound)
}

func TestDebitAndCredit_MoveExactlyTheAmountAndRejectNonPositiveAmounts(t *testing.T) {
	db, _ := newDB(t, postgres.Options{})
	ctx := context.Background()

	require.NoError(t, db.Wallets().Debit(ctx, "wallet_1", 30))
	require.NoError(t, db.Wallets().Credit(ctx, "wallet_2", 30))
	w1, err := db.Wallets().Get(ctx, "wallet_1")
	require.NoError(t, err)
	w2, err := db.Wallets().Get(ctx, "wallet_2")
	require.NoError(t, err)
	require.Equal(t, int64(70), w1.Balance)
	require.Equal(t, int64(30), w2.Balance)

	for _, amount := range []int64{0, -5} {
		require.Error(t, db.Wallets().Debit(ctx, "wallet_1", amount), "a negative debit would be a hidden credit")
		require.Error(t, db.Wallets().Credit(ctx, "wallet_2", amount), "a negative credit would be a hidden debit")
	}
	w1, err = db.Wallets().Get(ctx, "wallet_1")
	require.NoError(t, err)
	require.Equal(t, int64(70), w1.Balance)
}

func TestDebit_DatabaseRejectsOverdrawEvenWithoutALock(t *testing.T) {
	db, _ := newDB(t, postgres.Options{})

	err := db.Wallets().Debit(context.Background(), "wallet_1", 101) // balance is 100

	require.Error(t, err, "CHECK (balance >= 0) is the last line of defence")
}

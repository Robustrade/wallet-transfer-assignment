package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/repository"
	"wallet-transfer/internal/tests/testutils"
)

func TestRepo_EnqueueTransfer_Idempotent(t *testing.T) {
	pool, store := testutils.NewStore(t)
	ctx := context.Background()
	from := testutils.CreateWallet(t, pool, "100", true)
	to := testutils.CreateWallet(t, pool, "0", true)
	params := domain.NewTransferParams{
		IdempotencyKey: "it-" + uuid.NewString(), FromWalletID: from, ToWalletID: to, Amount: testutils.Money("30"),
	}

	first, created, err := store.EnqueueTransfer(ctx, params)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, domain.StatePending, first.State)

	second, created, err := store.EnqueueTransfer(ctx, params)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, first.ID, second.ID)
}

func TestRepo_EnqueueTransfer_UnknownWallet(t *testing.T) {
	pool, store := testutils.NewStore(t)
	_, _, err := store.EnqueueTransfer(context.Background(), domain.NewTransferParams{
		IdempotencyKey: "it-" + uuid.NewString(),
		FromWalletID:   uuid.New(),
		ToWalletID:     testutils.CreateWallet(t, pool, "0", true),
		Amount:         testutils.Money("10"),
	})
	require.ErrorIs(t, err, domain.ErrWalletNotFound)
}

func TestRepo_ClaimPending_MovesToProcessing(t *testing.T) {
	pool, store := testutils.NewStore(t)
	ctx := context.Background()
	from := testutils.CreateWallet(t, pool, "100", true)
	to := testutils.CreateWallet(t, pool, "0", true)
	enq, _, err := store.EnqueueTransfer(ctx, domain.NewTransferParams{
		IdempotencyKey: "it-" + uuid.NewString(), FromWalletID: from, ToWalletID: to, Amount: testutils.Money("10"),
	})
	require.NoError(t, err)

	claimed, err := store.ClaimPending(ctx, "worker-1", 50)
	require.NoError(t, err)
	var got *domain.Transfer
	for i := range claimed {
		if claimed[i].ID == enq.ID {
			got = &claimed[i]
		}
	}
	require.NotNil(t, got)
	require.Equal(t, domain.StateProcessing, got.State)
	require.Equal(t, 1, got.Attempts)
	require.Equal(t, "worker-1", *got.WorkerID)
}

func TestRepo_Tx_ProcessUpdatesBalancesAndLedger(t *testing.T) {
	pool, store := testutils.NewStore(t)
	ctx := context.Background()
	from := testutils.CreateWallet(t, pool, "100", true)
	to := testutils.CreateWallet(t, pool, "20", true)
	enq, _, err := store.EnqueueTransfer(ctx, domain.NewTransferParams{
		IdempotencyKey: "it-" + uuid.NewString(), FromWalletID: from, ToWalletID: to, Amount: testutils.Money("30"),
	})
	require.NoError(t, err)
	_, err = store.ClaimPending(ctx, "w", 50)
	require.NoError(t, err)

	err = store.Tx(ctx, func(tx repository.TxStore) error {
		tr, err := tx.GetTransferForUpdate(ctx, enq.ID)
		require.NoError(t, err)
		if _, _, err := tx.LockWalletPair(ctx, tr.FromWalletID, tr.ToWalletID); err != nil {
			return err
		}
		if err := tx.InsertLedgerEntry(ctx, domain.LedgerEntry{WalletID: from, TransferID: tr.ID, Type: domain.EntryDebit, Amount: tr.Amount}); err != nil {
			return err
		}
		if err := tx.InsertLedgerEntry(ctx, domain.LedgerEntry{WalletID: to, TransferID: tr.ID, Type: domain.EntryCredit, Amount: tr.Amount}); err != nil {
			return err
		}
		if err := tx.ApplyBalanceDelta(ctx, from, tr.Amount.Neg()); err != nil {
			return err
		}
		if err := tx.ApplyBalanceDelta(ctx, to, tr.Amount); err != nil {
			return err
		}
		return tx.SetProcessed(ctx, tr.ID)
	})
	require.NoError(t, err)
	require.True(t, testutils.Balance(t, pool, from).Equal(testutils.Money("70")))
	require.True(t, testutils.Balance(t, pool, to).Equal(testutils.Money("50")))

	final, err := store.GetTransferByID(ctx, enq.ID)
	require.NoError(t, err)
	require.Equal(t, domain.StateProcessed, final.State)
}

func TestRepo_Tx_DuplicateLedgerLegRejected(t *testing.T) {
	pool, store := testutils.NewStore(t)
	ctx := context.Background()
	from := testutils.CreateWallet(t, pool, "100", true)
	to := testutils.CreateWallet(t, pool, "0", true)
	enq, _, err := store.EnqueueTransfer(ctx, domain.NewTransferParams{
		IdempotencyKey: "it-" + uuid.NewString(), FromWalletID: from, ToWalletID: to, Amount: testutils.Money("10"),
	})
	require.NoError(t, err)

	err = store.Tx(ctx, func(tx repository.TxStore) error {
		leg := domain.LedgerEntry{WalletID: from, TransferID: enq.ID, Type: domain.EntryDebit, Amount: testutils.Money("10")}
		if err := tx.InsertLedgerEntry(ctx, leg); err != nil {
			return err
		}
		return tx.InsertLedgerEntry(ctx, leg)
	})
	require.ErrorIs(t, err, repository.ErrLedgerDuplicate)
}

func TestRepo_Tx_OverdraftBackstop(t *testing.T) {
	pool, store := testutils.NewStore(t)
	ctx := context.Background()
	from := testutils.CreateWallet(t, pool, "10", true)
	err := store.Tx(ctx, func(tx repository.TxStore) error {
		return tx.ApplyBalanceDelta(ctx, from, testutils.Money("-50"))
	})
	require.ErrorIs(t, err, domain.ErrInsufficientFunds)
}

func TestRepo_ReapStuck_Requeues(t *testing.T) {
	pool, store := testutils.NewStore(t)
	ctx := context.Background()
	from := testutils.CreateWallet(t, pool, "100", true)
	to := testutils.CreateWallet(t, pool, "0", true)
	enq, _, err := store.EnqueueTransfer(ctx, domain.NewTransferParams{
		IdempotencyKey: "it-" + uuid.NewString(), FromWalletID: from, ToWalletID: to, Amount: testutils.Money("10"),
	})
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`UPDATE transfer SET state='PROCESSING', claimed_at = now() - interval '1 hour', worker_id='dead' WHERE id=$1`, enq.ID)
	require.NoError(t, err)

	n, err := store.ReapStuck(ctx, 30*time.Second)
	require.NoError(t, err)
	require.GreaterOrEqual(t, n, int64(1))

	got, err := store.GetTransferByID(ctx, enq.ID)
	require.NoError(t, err)
	require.Equal(t, domain.StatePending, got.State)
}

func TestRepo_GetTransferByID_NotFound(t *testing.T) {
	_, store := testutils.NewStore(t)
	_, err := store.GetTransferByID(context.Background(), uuid.New())
	require.ErrorIs(t, err, domain.ErrTransferNotFound)
}

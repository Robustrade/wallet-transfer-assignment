package integration_test

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GaureshPatil/wallet-transfer-assignment/internal/domain"
	"github.com/GaureshPatil/wallet-transfer-assignment/internal/repository"
	"github.com/GaureshPatil/wallet-transfer-assignment/internal/service"
)

func transferInput(key, from, to string, amount int64) service.CreateTransferInput {
	return service.CreateTransferInput{IdempotencyKey: key, FromWalletID: from, ToWalletID: to, Amount: amount}
}

func TestTransfer_MovesMoneyAndWritesBalancedLedger(t *testing.T) {
	e := newEnv(t)
	e.wallet(t, "wallet_1", 500)
	e.wallet(t, "wallet_2", 50)

	res, err := e.transfers.CreateTransfer(context.Background(), transferInput("abc123", "wallet_1", "wallet_2", 100))

	require.NoError(t, err)
	assert.Equal(t, domain.TransferProcessed, res.Transfer.Status)
	assert.EqualValues(t, 400, e.balance(t, "wallet_1"))
	assert.EqualValues(t, 150, e.balance(t, "wallet_2"))

	details, err := e.transfers.GetTransfer(context.Background(), res.Transfer.ID)
	require.NoError(t, err)
	require.Len(t, details.Entries, 2, "every transfer produces exactly two ledger entries")
	assert.Equal(t, domain.EntryDebit, details.Entries[0].Type)
	assert.Equal(t, "wallet_1", details.Entries[0].WalletID)
	assert.Equal(t, domain.EntryCredit, details.Entries[1].Type)
	assert.Equal(t, "wallet_2", details.Entries[1].WalletID)
	assert.EqualValues(t, 100, details.Entries[0].Amount)
	assert.EqualValues(t, 100, details.Entries[1].Amount)
	e.assertLedgerReconciles(t)
}

func TestTransfer_SpendingTheExactBalanceIsAllowed(t *testing.T) {
	e := newEnv(t)
	e.wallet(t, "a", 100)
	e.wallet(t, "b", 0)

	res, err := e.transfers.CreateTransfer(context.Background(), transferInput("k", "a", "b", 100))

	require.NoError(t, err)
	assert.Equal(t, domain.TransferProcessed, res.Transfer.Status)
	assert.EqualValues(t, 0, e.balance(t, "a"))
	assert.EqualValues(t, 100, e.balance(t, "b"))
}

func TestTransfer_InsufficientFundsIsRecordedAsFailedAndMovesNoMoney(t *testing.T) {
	e := newEnv(t)
	e.wallet(t, "a", 99)
	e.wallet(t, "b", 0)

	res, err := e.transfers.CreateTransfer(context.Background(), transferInput("k", "a", "b", 100))

	require.NoError(t, err, "a business failure is a result, not an error")
	assert.Equal(t, domain.TransferFailed, res.Transfer.Status)
	assert.Equal(t, domain.FailureInsufficientFunds, res.Transfer.FailureReason)
	assert.EqualValues(t, 99, e.balance(t, "a"))
	assert.EqualValues(t, 0, e.balance(t, "b"))
	assert.Equal(t, 0, e.count(t, "ledger_entries"), "a failed transfer writes no ledger entries")

	details, err := e.transfers.GetTransfer(context.Background(), res.Transfer.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TransferFailed, details.Transfer.Status, "the failure is durably recorded")
	e.assertLedgerReconciles(t)
}

// Overflow is the one business failure detected after the source wallet has
// already been debited in memory: nothing of that partial change may be persisted.
func TestTransfer_DestinationOverflowIsRecordedAsFailedAndMovesNoMoney(t *testing.T) {
	e := newEnv(t)
	e.wallet(t, "src", 100)
	e.wallet(t, "full", math.MaxInt64)
	ctx := context.Background()

	first, err := e.transfers.CreateTransfer(ctx, transferInput("k", "src", "full", 1))

	require.NoError(t, err, "a business failure is a result, not an error")
	assert.False(t, first.Replayed)
	assert.Equal(t, domain.TransferFailed, first.Transfer.Status)
	assert.Equal(t, domain.FailureBalanceOverflow, first.Transfer.FailureReason)
	assert.EqualValues(t, 100, e.balance(t, "src"), "the in-memory debit must not be persisted")
	assert.EqualValues(t, math.MaxInt64, e.balance(t, "full"))
	assert.Equal(t, 0, e.count(t, "ledger_entries"), "a failed transfer writes no ledger entries")
	assert.Equal(t, 1, e.count(t, "transfers"))

	stored, err := e.transfers.GetTransfer(ctx, first.Transfer.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TransferFailed, stored.Transfer.Status, "the failure is durably recorded")
	assert.Equal(t, domain.FailureBalanceOverflow, stored.Transfer.FailureReason)
	assert.Empty(t, stored.Entries)

	replay, err := e.transfers.CreateTransfer(ctx, transferInput("k", "src", "full", 1))

	require.NoError(t, err)
	assert.True(t, replay.Replayed)
	assert.Equal(t, first.Transfer.ID, replay.Transfer.ID)
	assert.Equal(t, domain.TransferFailed, replay.Transfer.Status)
	assert.Equal(t, domain.FailureBalanceOverflow, replay.Transfer.FailureReason)
	assert.EqualValues(t, 100, e.balance(t, "src"))
	assert.Equal(t, 1, e.count(t, "transfers"), "the replay creates nothing new")
	e.assertLedgerReconciles(t)
}

func TestTransfer_UnknownWalletIsRejectedAndWritesNothing(t *testing.T) {
	e := newEnv(t)
	e.wallet(t, "a", 100)

	_, err := e.transfers.CreateTransfer(context.Background(), transferInput("k", "a", "ghost", 10))
	require.ErrorIs(t, err, domain.ErrWalletNotFound)
	_, err = e.transfers.CreateTransfer(context.Background(), transferInput("k2", "ghost", "a", 10))
	require.ErrorIs(t, err, domain.ErrWalletNotFound)

	assert.EqualValues(t, 100, e.balance(t, "a"))
	assert.Equal(t, 0, e.count(t, "transfers"))
	assert.Equal(t, 0, e.count(t, "ledger_entries"))
}

func TestTransfer_InvalidInputIsRejectedBeforeTouchingTheDatabase(t *testing.T) {
	e := newEnv(t)
	e.wallet(t, "a", 100)
	e.wallet(t, "b", 0)

	for name, in := range map[string]service.CreateTransferInput{
		"zero amount":     transferInput("k", "a", "b", 0),
		"negative amount": transferInput("k", "a", "b", -1),
		"same wallet":     transferInput("k", "a", "a", 1),
		"missing key":     transferInput("", "a", "b", 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := e.transfers.CreateTransfer(context.Background(), in)
			require.ErrorIs(t, err, domain.ErrInvalidRequest)
		})
	}
	assert.Equal(t, 0, e.count(t, "transfers"))
}

// failingLedgerStore makes every ledger write fail, simulating a crash after
// the balances were already updated inside the transaction.
type failingLedgerStore struct{ repository.Store }

func (s failingLedgerStore) WithinTx(ctx context.Context, fn func(repository.Repositories) error) error {
	return s.Store.WithinTx(ctx, func(r repository.Repositories) error {
		return fn(failingLedgerRepos{r})
	})
}

type failingLedgerRepos struct{ repository.Repositories }

func (failingLedgerRepos) Ledger() repository.LedgerRepository { return failingLedger{} }

type failingLedger struct{ repository.LedgerRepository }

func (failingLedger) Insert(context.Context, ...domain.LedgerEntry) error {
	return errors.New("simulated failure while writing the ledger")
}

func TestTransfer_PartialExecutionRollsBackEverything(t *testing.T) {
	healthy := newEnv(t)
	healthy.wallet(t, "a", 100)
	healthy.wallet(t, "b", 0)
	broken := newEnvWithStore(healthy.pool, failingLedgerStore{healthy.store})

	_, err := broken.transfers.CreateTransfer(context.Background(), transferInput("k", "a", "b", 40))

	require.Error(t, err)
	assert.EqualValues(t, 100, healthy.balance(t, "a"), "debit must be rolled back")
	assert.EqualValues(t, 0, healthy.balance(t, "b"), "credit must be rolled back")
	assert.Equal(t, 0, healthy.count(t, "transfers"), "no orphan transfer row")
	assert.Equal(t, 0, healthy.count(t, "ledger_entries"))
	healthy.assertLedgerReconciles(t)
}

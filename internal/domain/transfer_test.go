package domain_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wallet-transfer-assignment/internal/domain"
)

func baseTransfer() *domain.Transfer {
	return &domain.Transfer{
		ID:             uuid.New(),
		IdempotencyKey: "key-1",
		FromWalletID:   "wallet_a",
		ToWalletID:     "wallet_b",
		Amount:         100,
		Status:         domain.StatusPending,
	}
}

func TestTransfer_Validate(t *testing.T) {
	t.Run("valid transfer passes", func(t *testing.T) {
		require.NoError(t, baseTransfer().Validate())
	})

	t.Run("missing idempotency key", func(t *testing.T) {
		tr := baseTransfer()
		tr.IdempotencyKey = ""
		assert.ErrorIs(t, tr.Validate(), domain.ErrMissingIdempotency)
	})

	t.Run("same wallet rejected", func(t *testing.T) {
		tr := baseTransfer()
		tr.ToWalletID = tr.FromWalletID
		assert.ErrorIs(t, tr.Validate(), domain.ErrSameWallet)
	})

	t.Run("zero amount rejected", func(t *testing.T) {
		tr := baseTransfer()
		tr.Amount = 0
		assert.ErrorIs(t, tr.Validate(), domain.ErrAmountNotPositive)
	})

	t.Run("negative amount rejected", func(t *testing.T) {
		tr := baseTransfer()
		tr.Amount = -50
		assert.ErrorIs(t, tr.Validate(), domain.ErrAmountNotPositive)
	})

	t.Run("empty from-wallet rejected", func(t *testing.T) {
		tr := baseTransfer()
		tr.FromWalletID = ""
		assert.ErrorIs(t, tr.Validate(), domain.ErrMissingWalletID)
	})
}

func TestTransfer_StateMachine(t *testing.T) {
	t.Run("PENDING transitions to PROCESSED", func(t *testing.T) {
		tr := baseTransfer()
		require.NoError(t, tr.MarkProcessed())
		assert.Equal(t, domain.StatusProcessed, tr.Status)
	})

	t.Run("PENDING transitions to FAILED with reason", func(t *testing.T) {
		tr := baseTransfer()
		require.NoError(t, tr.MarkFailed("insufficient funds"))
		assert.Equal(t, domain.StatusFailed, tr.Status)
		assert.Equal(t, "insufficient funds", tr.FailureReason)
	})

	t.Run("PROCESSED cannot transition again", func(t *testing.T) {
		tr := baseTransfer()
		_ = tr.MarkProcessed()
		assert.Error(t, tr.MarkProcessed())
		assert.Error(t, tr.MarkFailed("oops"))
	})

	t.Run("FAILED cannot transition again", func(t *testing.T) {
		tr := baseTransfer()
		_ = tr.MarkFailed("reason")
		assert.Error(t, tr.MarkFailed("again"))
		assert.Error(t, tr.MarkProcessed())
	})
}

func TestNewLedgerPair(t *testing.T) {
	tid := uuid.New()
	debit, credit := domain.NewLedgerPair(tid, "wallet_a", "wallet_b", 250)

	assert.Equal(t, domain.EntryDebit, debit.Type)
	assert.Equal(t, "wallet_a", debit.WalletID)
	assert.Equal(t, int64(250), debit.Amount)

	assert.Equal(t, domain.EntryCredit, credit.Type)
	assert.Equal(t, "wallet_b", credit.WalletID)
	assert.Equal(t, int64(250), credit.Amount)

	assert.Equal(t, tid, debit.TransferID)
	assert.Equal(t, tid, credit.TransferID)

	// Entries must have distinct IDs.
	assert.NotEqual(t, debit.ID, credit.ID)
}

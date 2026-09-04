package unit

import (
	"testing"

	"wallet-transfer-assignment/internal/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWalletStateMutations(t *testing.T) {
	id := uuid.New()
	initial, _ := domain.ParseMoney("100.00")
	w, err := domain.NewWallet(id, initial, "USD")
	require.NoError(t, err)

	t.Run("successful debit and credit", func(t *testing.T) {
		debitAmt, _ := domain.ParseMoney("40.00")
		err := w.Debit(debitAmt)
		require.NoError(t, err)
		assert.Equal(t, "60.00", w.Balance.String())

		creditAmt, _ := domain.ParseMoney("20.00")
		err = w.Credit(creditAmt)
		require.NoError(t, err)
		assert.Equal(t, "80.00", w.Balance.String())
	})

	t.Run("insufficient funds debit", func(t *testing.T) {
		debitAmt, _ := domain.ParseMoney("1000.00")
		err := w.Debit(debitAmt)
		require.Error(t, err)
		var appErr *domain.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, domain.ErrCodeInsufficientFunds, appErr.Code)
		assert.Equal(t, "80.00", w.Balance.String()) // balance unchanged
	})
}

func TestTransferStateTransitions(t *testing.T) {
	fromID := uuid.New()
	toID := uuid.New()
	amt, _ := domain.ParseMoney("50.00")

	t.Run("same source and destination wallet rejection", func(t *testing.T) {
		_, err := domain.NewTransfer(uuid.New(), nil, fromID, fromID, amt)
		require.Error(t, err)
		var appErr *domain.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, domain.ErrCodeSameWalletTransfer, appErr.Code)
	})

	t.Run("pending to processed transition", func(t *testing.T) {
		tr, err := domain.NewTransfer(uuid.New(), nil, fromID, toID, amt)
		require.NoError(t, err)
		assert.Equal(t, domain.TransferStatusPending, tr.Status)

		err = tr.MarkProcessed()
		require.NoError(t, err)
		assert.Equal(t, domain.TransferStatusProcessed, tr.Status)
		assert.NotNil(t, tr.ProcessedAt)

		// Cannot transition back or to failed once processed
		err = tr.MarkFailed(domain.ErrCodeInternalError, "some error")
		require.Error(t, err)
	})

	t.Run("pending to failed transition", func(t *testing.T) {
		tr, err := domain.NewTransfer(uuid.New(), nil, fromID, toID, amt)
		require.NoError(t, err)

		err = tr.MarkFailed(domain.ErrCodeInsufficientFunds, "out of balance")
		require.NoError(t, err)
		assert.Equal(t, domain.TransferStatusFailed, tr.Status)

		err = tr.MarkProcessed()
		require.Error(t, err)
	})
}

package models

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNewLedgerPair(t *testing.T) {
	transferID := uuid.New()
	from := uuid.New()
	to := uuid.New()

	debit, credit := NewLedgerPair(transferID, from, to, 250)

	require.Equal(t, transferID, debit.TransferID)
	require.Equal(t, transferID, credit.TransferID)

	require.Equal(t, from, debit.WalletID)
	require.Equal(t, LedgerDebit, debit.Type)
	require.EqualValues(t, 250, debit.Amount)

	require.Equal(t, to, credit.WalletID)
	require.Equal(t, LedgerCredit, credit.Type)
	require.EqualValues(t, 250, credit.Amount)

	require.NotEqual(t, uuid.Nil, debit.ID)
	require.NotEqual(t, uuid.Nil, credit.ID)
	require.NotEqual(t, debit.ID, credit.ID, "each leg must get its own row id")
}

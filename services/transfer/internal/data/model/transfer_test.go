package model_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Robustrade/wallet-transfer-assignment/common/util/money"
	"github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/data/model"
)

func newPending() model.Transfer {
	return model.NewPendingTransfer(
		uuid.New(),
		"key-1",
		"hash-1",
		"wallet_a",
		"wallet_b",
		"INR",
		money.FromMinor(100),
	)
}

func TestTransfer_NewPendingTransfer_DefaultsToPending(t *testing.T) {
	tr := newPending()
	assert.Equal(t, model.StatusPending, tr.Status)
	assert.Nil(t, tr.FailureReason)
	assert.False(t, tr.IsTerminal())
}

func TestTransfer_MarkProcessed_FromPending(t *testing.T) {
	tr := newPending()
	require.True(t, tr.MarkProcessed())
	assert.Equal(t, model.StatusProcessed, tr.Status)
	assert.Nil(t, tr.FailureReason)
	assert.True(t, tr.IsTerminal())
}

func TestTransfer_MarkFailed_FromPending(t *testing.T) {
	tr := newPending()
	require.True(t, tr.MarkFailed("INSUFFICIENT_FUNDS"))
	assert.Equal(t, model.StatusFailed, tr.Status)
	require.NotNil(t, tr.FailureReason)
	assert.Equal(t, "INSUFFICIENT_FUNDS", *tr.FailureReason)
	assert.True(t, tr.IsTerminal())
}

func TestTransfer_MarkProcessed_RefusesFromTerminal(t *testing.T) {
	tr := newPending()
	require.True(t, tr.MarkProcessed())
	assert.False(t, tr.MarkProcessed())
	assert.False(t, tr.MarkFailed("nope"))
}

func TestTransfer_MarkFailed_RefusesFromTerminal(t *testing.T) {
	tr := newPending()
	require.True(t, tr.MarkFailed("INSUFFICIENT_FUNDS"))
	assert.False(t, tr.MarkProcessed())
	assert.False(t, tr.MarkFailed("again"))
	// FailureReason from the first call is preserved.
	require.NotNil(t, tr.FailureReason)
	assert.Equal(t, "INSUFFICIENT_FUNDS", *tr.FailureReason)
}

func TestTransfer_LedgerEntries_OneDebitOneCredit(t *testing.T) {
	tr := newPending()
	entries := tr.LedgerEntries()
	require.Len(t, entries, 2)

	// First entry is the DEBIT against the source wallet.
	assert.Equal(t, model.EntryDebit, entries[0].EntryType)
	assert.Equal(t, tr.FromWalletID, entries[0].WalletID)
	assert.Equal(t, tr.Amount, entries[0].Amount)
	assert.Equal(t, tr.ID, entries[0].TransferID)

	// Second entry is the CREDIT against the destination wallet.
	assert.Equal(t, model.EntryCredit, entries[1].EntryType)
	assert.Equal(t, tr.ToWalletID, entries[1].WalletID)
	assert.Equal(t, tr.Amount, entries[1].Amount)
	assert.Equal(t, tr.ID, entries[1].TransferID)
}

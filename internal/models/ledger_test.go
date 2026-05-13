package models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLedgerEntry_TableName(t *testing.T) {
	entry := LedgerEntry{}
	assert.Equal(t, "ledger_entries", entry.TableName())
}

func TestLedgerEntry_Fields(t *testing.T) {
	now := time.Now()
	entry := LedgerEntry{
		EntryID:    "entry-123",
		WalletID:   "wallet-1",
		TransferID: "transfer-123",
		Type:       EntryTypeDebit,
		Amount:     100,
		CreatedAt:  now,
	}

	assert.Equal(t, "entry-123", entry.EntryID)
	assert.Equal(t, "wallet-1", entry.WalletID)
	assert.Equal(t, "transfer-123", entry.TransferID)
	assert.Equal(t, EntryTypeDebit, entry.Type)
	assert.Equal(t, int64(100), entry.Amount)
}

func TestEntryTypeConstants(t *testing.T) {
	assert.Equal(t, "DEBIT", EntryTypeDebit)
	assert.Equal(t, "CREDIT", EntryTypeCredit)
}

func TestLedgerEntry_DebitEntry(t *testing.T) {
	entry := LedgerEntry{
		Type:   EntryTypeDebit,
		Amount: 50,
	}
	assert.Equal(t, EntryTypeDebit, entry.Type)
	assert.Equal(t, int64(50), entry.Amount)
}

func TestLedgerEntry_CreditEntry(t *testing.T) {
	entry := LedgerEntry{
		Type:   EntryTypeCredit,
		Amount: 75,
	}
	assert.Equal(t, EntryTypeCredit, entry.Type)
	assert.Equal(t, int64(75), entry.Amount)
}

func TestLedgerEntry_WithZeroAmount(t *testing.T) {
	entry := LedgerEntry{
		Type:   EntryTypeDebit,
		Amount: 0,
	}
	assert.Equal(t, int64(0), entry.Amount)
}

func TestLedgerEntry_WithLargeAmount(t *testing.T) {
	entry := LedgerEntry{
		Type:   EntryTypeCredit,
		Amount: 999999999999,
	}
	assert.Equal(t, int64(999999999999), entry.Amount)
}

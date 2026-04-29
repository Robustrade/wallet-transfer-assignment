package domain

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewWallet(t *testing.T) {
	wallet := NewWallet()

	require.NotNil(t, wallet)
	assert.NotEqual(t, uuid.Nil, wallet.ID, "ID should not be nil UUID")
	assert.Equal(t, int64(0), wallet.Balance, "New wallet should have zero balance")
	assert.False(t, wallet.CreatedAt.IsZero(), "CreatedAt should be set")
	assert.False(t, wallet.UpdatedAt.IsZero(), "UpdatedAt should be set")
}

func TestNewWallet_UniqueIDs(t *testing.T) {
	wallet1 := NewWallet()
	wallet2 := NewWallet()

	assert.NotEqual(t, wallet1.ID, wallet2.ID, "Each wallet should have unique ID")
}

func TestNewTransfer_Success(t *testing.T) {
	fromWalletID := uuid.New()
	toWalletID := uuid.New()
	transfer, err := NewTransfer(fromWalletID, toWalletID, 100)

	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, transfer.ID)
	assert.Equal(t, fromWalletID, transfer.FromWalletID)
	assert.Equal(t, toWalletID, transfer.ToWalletID)
	assert.Equal(t, int64(100), transfer.Amount)
	assert.Equal(t, TransferStatePending, transfer.State)
}

func TestNewTransfer_InvalidAmount(t *testing.T) {
	tests := []struct {
		name   string
		amount int64
	}{
		{"zero amount", 0},
		{"negative amount", -100},
	}

	fromWalletID := uuid.New()
	toWalletID := uuid.New()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transfer, err := NewTransfer(fromWalletID, toWalletID, tt.amount)
			assert.Error(t, err)
			assert.Equal(t, ErrInvalidAmount, err)
			assert.Nil(t, transfer)
		})
	}
}

func TestNewTransfer_SameWallet(t *testing.T) {
	walletID := uuid.New()
	transfer, err := NewTransfer(walletID, walletID, 100)

	assert.Error(t, err)
	assert.Equal(t, ErrSameWallet, err)
	assert.Nil(t, transfer)
}

func TestTransfer_MarkAsProcessed(t *testing.T) {
	fromWalletID := uuid.New()
	toWalletID := uuid.New()
	transfer, _ := NewTransfer(fromWalletID, toWalletID, 100)

	err := transfer.MarkAsProcessed()

	require.NoError(t, err)
	assert.Equal(t, TransferStateProcessed, transfer.State)
}

func TestTransfer_MarkAsFailed(t *testing.T) {
	fromWalletID := uuid.New()
	toWalletID := uuid.New()
	transfer, _ := NewTransfer(fromWalletID, toWalletID, 100)

	err := transfer.MarkAsFailed()

	require.NoError(t, err)
	assert.Equal(t, TransferStateFailed, transfer.State)
}

func TestTransfer_InvalidStateTransition(t *testing.T) {
	tests := []struct {
		name          string
		initialState  TransferState
		transition    func(*Transfer) error
		expectedError error
	}{
		{
			name:         "processed to processed",
			initialState: TransferStateProcessed,
			transition: func(t *Transfer) error {
				return t.MarkAsProcessed()
			},
			expectedError: ErrInvalidStateTransition,
		},
		{
			name:         "processed to failed",
			initialState: TransferStateProcessed,
			transition: func(t *Transfer) error {
				return t.MarkAsFailed()
			},
			expectedError: ErrInvalidStateTransition,
		},
		{
			name:         "failed to processed",
			initialState: TransferStateFailed,
			transition: func(t *Transfer) error {
				return t.MarkAsProcessed()
			},
			expectedError: ErrInvalidStateTransition,
		},
		{
			name:         "failed to failed",
			initialState: TransferStateFailed,
			transition: func(t *Transfer) error {
				return t.MarkAsFailed()
			},
			expectedError: ErrInvalidStateTransition,
		},
	}

	fromWalletID := uuid.New()
	toWalletID := uuid.New()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transfer, _ := NewTransfer(fromWalletID, toWalletID, 100)
			transfer.State = tt.initialState

			err := tt.transition(transfer)

			assert.Error(t, err)
			assert.Equal(t, tt.expectedError, err)
		})
	}
}

func TestTransfer_IsTerminal(t *testing.T) {
	tests := []struct {
		name     string
		state    TransferState
		expected bool
	}{
		{"pending is not terminal", TransferStatePending, false},
		{"processed is terminal", TransferStateProcessed, true},
		{"failed is terminal", TransferStateFailed, true},
	}

	fromWalletID := uuid.New()
	toWalletID := uuid.New()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transfer, _ := NewTransfer(fromWalletID, toWalletID, 100)
			transfer.State = tt.state

			assert.Equal(t, tt.expected, transfer.IsTerminal())
		})
	}
}

func TestWallet_CanDebit(t *testing.T) {
	tests := []struct {
		name     string
		balance  int64
		amount   int64
		expected bool
	}{
		{"sufficient funds", 1000, 500, true},
		{"exact balance", 1000, 1000, true},
		{"insufficient funds", 1000, 1500, false},
		{"zero balance", 0, 100, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wallet := &Wallet{
				ID:      uuid.New(),
				Balance: tt.balance,
			}

			assert.Equal(t, tt.expected, wallet.CanDebit(tt.amount))
		})
	}
}

func TestNewLedgerEntry_Success(t *testing.T) {
	tests := []struct {
		name      string
		entryType EntryType
	}{
		{"debit entry", EntryTypeDebit},
		{"credit entry", EntryTypeCredit},
	}

	walletID := uuid.New()
	transferID := uuid.New()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, err := NewLedgerEntry(walletID, transferID, tt.entryType, 100)

			require.NoError(t, err)
			assert.Equal(t, walletID, entry.WalletID)
			assert.Equal(t, transferID, entry.TransferID)
			assert.Equal(t, tt.entryType, entry.EntryType)
			assert.Equal(t, int64(100), entry.Amount)
		})
	}
}

func TestNewLedgerEntry_InvalidAmount(t *testing.T) {
	tests := []struct {
		name   string
		amount int64
	}{
		{"zero amount", 0},
		{"negative amount", -100},
	}

	walletID := uuid.New()
	transferID := uuid.New()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, err := NewLedgerEntry(walletID, transferID, EntryTypeDebit, tt.amount)

			assert.Error(t, err)
			assert.Equal(t, ErrInvalidAmount, err)
			assert.Nil(t, entry)
		})
	}
}

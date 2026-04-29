package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// TransferState represents the state of a transfer
type TransferState string

const (
	TransferStatePending   TransferState = "PENDING"
	TransferStateProcessed TransferState = "PROCESSED"
	TransferStateFailed    TransferState = "FAILED"
)

// EntryType represents the type of ledger entry
type EntryType string

const (
	EntryTypeDebit  EntryType = "DEBIT"
	EntryTypeCredit EntryType = "CREDIT"
)

// Common errors
var (
	ErrInsufficientFunds      = errors.New("insufficient funds")
	ErrInvalidAmount          = errors.New("amount must be positive")
	ErrSameWallet             = errors.New("cannot transfer to same wallet")
	ErrInvalidState           = errors.New("invalid transfer state")
	ErrWalletNotFound         = errors.New("wallet not found")
	ErrTransferNotFound       = errors.New("transfer not found")
	ErrInvalidStateTransition = errors.New("invalid state transition")
)

// Wallet represents a wallet entity
type Wallet struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;column:id;default:gen_random_uuid()"`
	Balance   int64     `gorm:"column:balance"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (Wallet) TableName() string {
	return "wallets"
}

// NewWallet creates a new wallet with a unique UUID
func NewWallet() *Wallet {
	now := time.Now()
	return &Wallet{
		ID:        uuid.New(),
		Balance:   0,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// CanDebit checks if wallet has sufficient funds for a debit
func (w *Wallet) CanDebit(amount int64) bool {
	return w.Balance >= amount
}

// Transfer represents a transfer entity
type Transfer struct {
	ID           uuid.UUID     `gorm:"type:uuid;primaryKey;column:id;default:gen_random_uuid()"`
	FromWalletID uuid.UUID     `gorm:"type:uuid;column:from_wallet_id"`
	ToWalletID   uuid.UUID     `gorm:"type:uuid;column:to_wallet_id"`
	Amount       int64         `gorm:"column:amount"`
	State        TransferState `gorm:"column:state"`
	CreatedAt    time.Time     `gorm:"column:created_at"`
	UpdatedAt    time.Time     `gorm:"column:updated_at"`
}

func (Transfer) TableName() string {
	return "transfers"
}

// NewTransfer creates a new transfer with validation
func NewTransfer(fromWalletID, toWalletID uuid.UUID, amount int64) (*Transfer, error) {
	if amount <= 0 {
		return nil, ErrInvalidAmount
	}
	if fromWalletID == toWalletID {
		return nil, ErrSameWallet
	}

	now := time.Now()
	return &Transfer{
		ID:           uuid.New(),
		FromWalletID: fromWalletID,
		ToWalletID:   toWalletID,
		Amount:       amount,
		State:        TransferStatePending,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

// MarkAsProcessed transitions transfer to processed state
func (t *Transfer) MarkAsProcessed() error {
	if t.State != TransferStatePending {
		return ErrInvalidStateTransition
	}
	t.State = TransferStateProcessed
	t.UpdatedAt = time.Now()
	return nil
}

// MarkAsFailed transitions transfer to failed state
func (t *Transfer) MarkAsFailed() error {
	if t.State != TransferStatePending {
		return ErrInvalidStateTransition
	}
	t.State = TransferStateFailed
	t.UpdatedAt = time.Now()
	return nil
}

// IsTerminal checks if the transfer is in a terminal state
func (t *Transfer) IsTerminal() bool {
	return t.State == TransferStateProcessed || t.State == TransferStateFailed
}

// LedgerEntry represents a ledger entry
type LedgerEntry struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey;column:id;default:gen_random_uuid()"`
	WalletID   uuid.UUID `gorm:"type:uuid;column:wallet_id"`
	TransferID uuid.UUID `gorm:"type:uuid;column:transfer_id"`
	EntryType  EntryType `gorm:"column:entry_type"`
	Amount     int64     `gorm:"column:amount"`
	CreatedAt  time.Time `gorm:"column:created_at"`
}

func (LedgerEntry) TableName() string {
	return "ledger_entries"
}

// NewLedgerEntry creates a new ledger entry with validation
func NewLedgerEntry(walletID, transferID uuid.UUID, entryType EntryType, amount int64) (*LedgerEntry, error) {
	if amount <= 0 {
		return nil, ErrInvalidAmount
	}

	return &LedgerEntry{
		ID:         uuid.New(),
		WalletID:   walletID,
		TransferID: transferID,
		EntryType:  entryType,
		Amount:     amount,
		CreatedAt:  time.Now(),
	}, nil
}

// IdempotencyRecord represents an idempotency record
type IdempotencyRecord struct {
	IdempotencyKey uuid.UUID `gorm:"type:uuid;primaryKey;column:idempotency_key"`
	TransferID     uuid.UUID `gorm:"type:uuid;column:transfer_id"`
	ResponseData   []byte    `gorm:"column:response_data;type:jsonb"`
	CreatedAt      time.Time `gorm:"column:created_at"`
}

func (IdempotencyRecord) TableName() string {
	return "idempotency_records"
}

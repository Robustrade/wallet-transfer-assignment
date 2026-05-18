package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

type LedgerEntryType string

type TransactionStatus string

const (
	EntryDebit      LedgerEntryType   = "debit"
	EntryCredit     LedgerEntryType   = "credit"
	StatusPending   TransactionStatus = "PENDING"
	StatusCompleted TransactionStatus = "PROCESSED"
	StatusFailed    TransactionStatus = "FAILED"
)

type Wallet struct {
	ID        uint64          `json:"id"`
	UserID    uint64          `json:"user_id"`
	Currency  string          `json:"currency"`
	Balance   decimal.Decimal `json:"balance"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type Transaction struct {
	ID             uint64            `json:"id"`
	IdempotencyKey string            `json:"idempotency_key"`
	ReferenceID    string            `json:"reference_id"`
	FromWalletID   uint64            `json:"from_wallet_id"`
	ToWalletID     uint64            `json:"to_wallet_id"`
	Amount         decimal.Decimal   `json:"amount"`
	Currency       string            `json:"currency"`
	Status         TransactionStatus `json:"status"`
	Description    string            `json:"description,omitempty"`
	Metadata       string            `json:"metadata,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

type LedgerEntry struct {
	ID            uint64          `json:"id"`
	TransactionID uint64          `json:"transaction_id"`
	WalletID      uint64          `json:"wallet_id"`
	EntryType     LedgerEntryType `json:"entry_type"`
	Amount        decimal.Decimal `json:"amount"`
	BalanceBefore decimal.Decimal `json:"balance_before"`
	BalanceAfter  decimal.Decimal `json:"balance_after"`
	CreatedAt     time.Time       `json:"created_at"`
}

type OutboxEvent struct {
	ID        uint64    `json:"id"`
	EventType string    `json:"event_type"`
	Payload   string    `json:"payload"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

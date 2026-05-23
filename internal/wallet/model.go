package wallet

import "time"

type TransactionStateType string
type WalletStatusType string
type LedgerEntryType string

const (
	StateProcessed TransactionStateType = "PROCESSED"
	StatePending   TransactionStateType = "PENDING"
	StateFailed    TransactionStateType = "FAILED"
)

const (
	LedgerEntryCredit LedgerEntryType = "CREDIT"
	LedgerEntryDebit  LedgerEntryType = "DEBIT"
)

const (
	WalletStatusActive   WalletStatusType = "ACTIVE"
	WalletStatusInactive WalletStatusType = "INACTIVE"
)

type Wallet struct {
	WalletId  string           `db:"wallet_id"`
	Balance   int64            `db:"balance"`
	Status    WalletStatusType `db:"status"`
	CreatedAt time.Time        `db:"created_at"`
	UpdatedAt time.Time        `db:"updated_at"`
}

type Transfer struct {
	TransactionId  string               `db:"transaction_id"`
	IdempotencyKey string               `db:"idempotency_key"`
	FromWalletId   string               `db:"from_wallet_id"`
	ToWalletId     string               `db:"to_wallet_id"`
	Amount         int64                `db:"amount"`
	State          TransactionStateType `db:"status"`
	CreatedAt      time.Time            `db:"created_at"`
	UpdatedAt      time.Time            `db:"updated_at"`
}

type LedgerEntry struct {
	TransactionId string          `db:"transaction_id"`
	EntryId       string          `db:"entry_id"`
	WalletId      string          `db:"wallet_id"`
	Amount        int64           `db:"amount"`
	Entry         LedgerEntryType `db:"entry"`
	BalanceBefore int64           `db:"balance_before"`
	BalanceAfter  int64           `db:"balance_after"`
	CreatedAt     time.Time       `db:"created_at"`
	UpdatedAt     time.Time       `db:"updated_at"`
}

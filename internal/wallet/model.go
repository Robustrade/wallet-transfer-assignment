package wallet

import "time"

type TransactionState string

const (
	StatePending   TransactionState = "PENDING"
	StateProcessed TransactionState = "PROCESSED"
	StateFailed    TransactionState = "FAILED"
)

type LedgerEntryType string

const (
	EntryDebit  LedgerEntryType = "DEBIT"
	EntryCredit LedgerEntryType = "CREDIT"
)

type Wallet struct {
	WalletID  int64     `db:"wallet_id"`
	Balance   int64     `db:"balance"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

type Transaction struct {
	ID             int64            `db:"id"`
	IdempotencyKey string           `db:"idempotency_key"`
	FromWalletID   int64            `db:"from_wallet_id"`
	ToWalletID     int64            `db:"to_wallet_id"`
	Amount         int64            `db:"amount"`
	State          TransactionState `db:"state"`
	CreatedAt      time.Time        `db:"created_at"`
	UpdatedAt      time.Time        `db:"updated_at"`
}

type LedgerEntry struct {
	EntryID       int64           `db:"entry_id"`
	TransactionID int64           `db:"transaction_id"`
	WalletID      int64           `db:"wallet_id"`
	Type          LedgerEntryType `db:"type"`
	Amount        int64           `db:"amount"`
	CreatedAt     time.Time       `db:"created_at"`
}

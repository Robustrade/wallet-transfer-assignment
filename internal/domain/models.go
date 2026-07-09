package domain

import "time"

type TransferState string

const (
	TransferPending   TransferState = "PENDING"
	TransferProcessed TransferState = "PROCESSED"
	TransferFailed    TransferState = "FAILED"
)

type LedgerEntryType string

const (
	LedgerDebit  LedgerEntryType = "DEBIT"
	LedgerCredit LedgerEntryType = "CREDIT"
)

type Transfer struct {
	ID             string
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	State          TransferState
	FailureReason  string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Wallet struct {
	ID        string
	Balance   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

type LedgerEntry struct {
	ID         int64
	WalletID   string
	TransferID string
	Type       LedgerEntryType
	Amount     int64
	CreatedAt  time.Time
}

type IdempotencyRecord struct {
	Key         string
	RequestHash string
	TransferID  string
	CreatedAt   time.Time
}

func (t Transfer) CanTransitionTo(next TransferState) bool {
	if t.State != TransferPending {
		return false
	}
	return next == TransferProcessed || next == TransferFailed
}

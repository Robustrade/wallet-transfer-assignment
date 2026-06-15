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

type Wallet struct {
	ID        string
	Balance   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Transfer struct {
	ID             string
	IdempotencyKey string
	RequestHash    string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	State          TransferState
	ErrorCode      ErrorCode
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type LedgerEntry struct {
	ID         int64
	WalletID   string
	TransferID string
	Type       LedgerEntryType
	Amount     int64
	CreatedAt  time.Time
}

type TransferResult struct {
	Transfer         Transfer
	LedgerEntries    []LedgerEntry
	IdempotentReplay bool
}

type CreateTransferInput struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

type CreateWalletInput struct {
	ID             string
	InitialBalance int64
}

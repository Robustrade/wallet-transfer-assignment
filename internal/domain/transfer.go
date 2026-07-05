package domain

import "time"

// TransferState is the transfer state machine. There are only three states
// and both non-PENDING states are terminal — a transfer is never retried
// in place, a fresh idempotencyKey-guarded attempt is made instead.
type TransferState string

const (
	TransferPending   TransferState = "PENDING"
	TransferProcessed TransferState = "PROCESSED"
	TransferFailed    TransferState = "FAILED"
)

// CanTransitionTo enforces the allowed edges of the transfer state machine:
//
//	PENDING -> PROCESSED
//	PENDING -> FAILED
//
// PROCESSED and FAILED are terminal; nothing transitions out of them.
func (s TransferState) CanTransitionTo(next TransferState) bool {
	if s != TransferPending {
		return false
	}
	return next == TransferProcessed || next == TransferFailed
}

// Transfer is a single wallet-to-wallet money movement request.
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

// LedgerEntryType distinguishes the two sides of a double-entry posting.
type LedgerEntryType string

const (
	LedgerDebit  LedgerEntryType = "DEBIT"
	LedgerCredit LedgerEntryType = "CREDIT"
)

// LedgerEntry is one leg of a double-entry ledger posting. Every processed
// transfer produces exactly two of these (one DEBIT, one CREDIT) with equal
// amounts, so the ledger always balances by construction.
type LedgerEntry struct {
	ID         string
	WalletID   string
	TransferID string
	Type       LedgerEntryType
	Amount     int64
	CreatedAt  time.Time
}

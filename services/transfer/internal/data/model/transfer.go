package model

import (
	"time"

	"github.com/google/uuid"

	"github.com/Robustrade/wallet-transfer-assignment/common/util/money"
)

// TransferStatus is the lifecycle state of a transfer.
//
// PENDING is the row's initial in-transaction state and is never observed in
// committed state under normal operation: every transaction either updates to
// PROCESSED / FAILED before commit, or is rolled back entirely.
type TransferStatus string

// Allowed values for TransferStatus.
const (
	StatusPending   TransferStatus = "PENDING"
	StatusProcessed TransferStatus = "PROCESSED"
	StatusFailed    TransferStatus = "FAILED"
)

// FailureReason is the canonical reason a transfer ended in FAILED state.
// Stored verbatim in transfers.failure_reason and surfaced to clients via
// TransferResp.FailureReason.
type FailureReason string

// Allowed values for FailureReason.
const (
	ReasonInsufficientFunds FailureReason = "INSUFFICIENT_FUNDS"
	ReasonCurrencyMismatch  FailureReason = "CURRENCY_MISMATCH"
)

// Transfer is the domain entity for a wallet-to-wallet transfer attempt.
type Transfer struct {
	ID             uuid.UUID
	IdempotencyKey string
	RequestHash    string
	FromWalletID   string
	ToWalletID     string
	Amount         money.Money
	Currency       string
	Status         TransferStatus
	FailureReason  *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewPendingTransfer constructs a Transfer in the PENDING state.
func NewPendingTransfer(
	id uuid.UUID,
	idempotencyKey, requestHash, fromWalletID, toWalletID, currency string,
	amount money.Money,
) Transfer {
	return Transfer{
		ID:             id,
		IdempotencyKey: idempotencyKey,
		RequestHash:    requestHash,
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         amount,
		Currency:       currency,
		Status:         StatusPending,
	}
}

// MarkProcessed transitions the transfer to PROCESSED and clears any failure reason.
// Returns false if the transition is not allowed from the current state.
func (t *Transfer) MarkProcessed() bool {
	if t.Status != StatusPending {
		return false
	}
	t.Status = StatusProcessed
	t.FailureReason = nil
	return true
}

// MarkFailed transitions the transfer to FAILED with the supplied reason.
// Returns false if the transition is not allowed from the current state.
func (t *Transfer) MarkFailed(reason FailureReason) bool {
	if t.Status != StatusPending {
		return false
	}
	t.Status = StatusFailed
	r := string(reason)
	t.FailureReason = &r
	return true
}

// IsTerminal reports whether the transfer is in a committed terminal state.
func (t *Transfer) IsTerminal() bool {
	return t.Status == StatusProcessed || t.Status == StatusFailed
}

// LedgerEntries returns the two ledger entries that materialize this transfer.
// The order is (DEBIT from-wallet, CREDIT to-wallet).
func (t *Transfer) LedgerEntries() []LedgerEntry {
	return []LedgerEntry{
		{
			TransferID: t.ID,
			WalletID:   t.FromWalletID,
			EntryType:  EntryDebit,
			Amount:     t.Amount,
		},
		{
			TransferID: t.ID,
			WalletID:   t.ToWalletID,
			EntryType:  EntryCredit,
			Amount:     t.Amount,
		},
	}
}

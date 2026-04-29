package transfer

import (
	"time"

	"wallet-transfer/internal/wallet"
)

// Transfer represents a wallet-to-wallet transfer.
type Transfer struct {
	ID             string          `json:"id"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	FromWalletID   string          `json:"from_wallet_id"`
	ToWalletID     string          `json:"to_wallet_id"`
	Amount         float64         `json:"amount"`
	Status         string          `json:"status"`
	FailureReason  string          `json:"failure_reason,omitempty"`
	AuditLog       wallet.AuditLog `json:"audit_log"`
}

// LedgerEntry represents one side of a double-entry ledger record.
type LedgerEntry struct {
	ID         string    `json:"id"`
	WalletID   string    `json:"wallet_id"`
	TransferID string    `json:"transfer_id"`
	Type       string    `json:"type"` // DEBIT | CREDIT
	Amount     float64   `json:"amount"`
	CreatedBy  string    `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`
}

// CreateTransferRequest is the request body for POST /transfers.
type CreateTransferRequest struct {
	IdempotencyKey string  `json:"idempotencyKey"`
	FromWalletID   string  `json:"fromWalletId" validate:"required"`
	ToWalletID     string  `json:"toWalletId" validate:"required"`
	Amount         float64 `json:"amount" validate:"required,gt=0"`
}

// TransferResponse is the API response for a transfer.
type TransferResponse struct {
	Transfer *Transfer      `json:"transfer"`
	Ledger   []*LedgerEntry `json:"ledger_entries"`
}

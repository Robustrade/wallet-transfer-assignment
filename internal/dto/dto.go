// Package dto defines the JSON wire shapes shared by the HTTP layer and the
// service layer. The service renders recorded outcomes (201/422/404) from
// these types so that replays are byte-identical to the original response.
package dto

import (
	"time"

	"wallet/internal/domain"
)

// TransferRequest is the canonical form of a POST /transfers body. Handlers
// decode into a pointer-field twin (to detect missing fields), then build this
// struct and marshal it to obtain the canonical bytes used for idempotency
// comparison.
type TransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

// TransferResponse is the body of a successful (201) transfer, and of
// GET /transfers/{id}.
type TransferResponse struct {
	TransferID    string `json:"transferId"`
	Status        string `json:"status"`
	FromWalletID  string `json:"fromWalletId"`
	ToWalletID    string `json:"toWalletId"`
	Amount        int64  `json:"amount"`
	FailureReason string `json:"failureReason,omitempty"`
	CreatedAt     string `json:"createdAt"`
}

// NewTransferResponse renders a transfer for the wire.
func NewTransferResponse(t domain.Transfer) TransferResponse {
	return TransferResponse{
		TransferID:    t.ID,
		Status:        string(t.Status),
		FromWalletID:  t.FromWalletID,
		ToWalletID:    t.ToWalletID,
		Amount:        t.Amount,
		FailureReason: t.FailureReason,
		CreatedAt:     FormatTime(t.CreatedAt),
	}
}

// WalletResponse is the body of GET /wallets/{id}.
type WalletResponse struct {
	WalletID string `json:"walletId"`
	Balance  int64  `json:"balance"`
	Currency string `json:"currency"`
}

// LedgerEntryResponse is one row of GET /wallets/{id}/ledger.
type LedgerEntryResponse struct {
	EntryID    int64  `json:"entryId"`
	TransferID string `json:"transferId"`
	WalletID   string `json:"walletId"`
	Type       string `json:"type"`
	Amount     int64  `json:"amount"`
	CreatedAt  string `json:"createdAt"`
}

// ErrorResponse is the body of every error response.
type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody carries the machine-readable code and a human-readable message.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// TransferID is set on INSUFFICIENT_FUNDS (the persisted FAILED transfer).
	TransferID string `json:"transferId,omitempty"`
}

// NewErrorResponse builds an error body without an attached transfer.
func NewErrorResponse(code, message string) ErrorResponse {
	return ErrorResponse{Error: ErrorBody{Code: code, Message: message}}
}

// FormatTime renders timestamps as RFC 3339 with nanoseconds, UTC.
func FormatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

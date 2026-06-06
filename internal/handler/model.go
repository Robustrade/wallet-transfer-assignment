package handler

import (
	"github.com/google/uuid"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

// ── Request / Response types ──────────────────────────────────────────────────
// View models for the handler layer. These are separate from the domain models to
// allow the handler to evolve independently of the domain, and to decouple the
// handler from any internal changes to the domain models. They also allow us to
// control exactly what fields are exposed in the API, and to perform any necessary
// transformations between the domain and the API layer.

// CreateTransferRequest is the JSON body for POST /transfers.
type CreateTransferRequest struct {
	IdempotencyKey string        `json:"idempotencyKey"`
	FromWalletID   uuid.UUID     `json:"fromWalletId"`
	ToWalletID     uuid.UUID     `json:"toWalletId"`
	Amount         domain.Amount `json:"amount"`
}

// MagicDepositRequest is the JSON body for POST /magic.
// Unlike CreateTransferRequest it omits fromWalletId and amount; the service
// layer fills those in automatically (nature wallet + random amount).
type MagicDepositRequest struct {
	IdempotencyKey string    `json:"idempotencyKey"`
	ToWalletID     uuid.UUID `json:"toWalletId"`
}

// CreateTransferResponse is the JSON body returned on 201 / 200.
type CreateTransferResponse struct {
	ID     uuid.UUID             `json:"id"`
	Status domain.TransferStatus `json:"status"`
	Amount domain.Amount         `json:"amount"`
}

// WalletResponse is the JSON body returned by GET /wallets/{id}.
type WalletResponse struct {
	ID      uuid.UUID     `json:"id"`
	Balance domain.Amount `json:"balance"`
}

// TransferResponse is the JSON body returned by GET /transfers/{id}.
type TransferResponse struct {
	ID     uuid.UUID             `json:"id"`
	Status domain.TransferStatus `json:"status"`
	Amount domain.Amount         `json:"amount"`
}

// errorResponse is the JSON body for all error responses.
type errorResponse struct {
	RequestID string `json:"requestId,omitempty"`
	Error     string `json:"error"`
}

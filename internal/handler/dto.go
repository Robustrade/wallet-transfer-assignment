package handler

import "encoding/json"

// InitTransferResponse is the response body of POST /transfers/init.
type InitTransferResponse struct {
	TransactionID string `json:"transactionId"`
}

// CreateTransferRequest is the request body of POST /transfers. Amount
// decodes as json.Number, never float64 — see docs/DESIGN.md §11.
type CreateTransferRequest struct {
	IdempotencyKey string      `json:"idempotencyKey"`
	FromWalletID   string      `json:"fromWalletId"`
	ToWalletID     string      `json:"toWalletId"`
	Amount         json.Number `json:"amount"`
}

// TransferView is the shared response representation for both
// POST /transfers and GET /transfers/{id} — see docs/DESIGN.md §1.
type TransferView struct {
	ID            string  `json:"id"`
	FromWalletID  string  `json:"fromWalletId"`
	ToWalletID    string  `json:"toWalletId"`
	Amount        string  `json:"amount"`
	Status        string  `json:"status"`
	FailureReason *string `json:"failureReason,omitempty"`
	CreatedAt     string  `json:"createdAt"`
}

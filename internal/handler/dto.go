package handler

// ErrorResponse is the standard API error payload.
type ErrorResponse struct {
	Error string `json:"error" example:"invalid request"`
}

// HealthResponse is returned by GET /health.
type HealthResponse struct {
	Status string `json:"status" example:"ok"`
}

// WalletBalanceResponse is returned by GET /wallets/{id}/balance.
type WalletBalanceResponse struct {
	WalletID string `json:"walletId" example:"wallet_1"`
	Balance  int64  `json:"balance" example:"10000"`
}

// CreateTransferRequest is the body for POST /transfers.
type CreateTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey" example:"abc123"`
	FromWalletID   string `json:"fromWalletId" example:"wallet_1"`
	ToWalletID     string `json:"toWalletId" example:"wallet_2"`
	Amount         int64  `json:"amount" example:"100"`
}

// TransferResponse is returned by POST /transfers.
type TransferResponse struct {
	TransferID     string  `json:"transferId" example:"550e8400-e29b-41d4-a716-446655440000"`
	Status         string  `json:"status" example:"PROCESSED"`
	FromWalletID   string  `json:"fromWalletId" example:"wallet_1"`
	ToWalletID     string  `json:"toWalletId" example:"wallet_2"`
	Amount         int64   `json:"amount" example:"100"`
	LedgerEntryIDs []int64 `json:"ledgerEntryIds,omitempty" example:"1,2"`
	FailureReason  string  `json:"failureReason,omitempty" example:"insufficient funds"`
}

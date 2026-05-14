package dto

// CreateTransferRequest represents the API request to create a transfer
type CreateTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
	Description    string `json:"description,omitempty"`
}

// TransferResponse represents the API response for a transfer
type TransferResponse struct {
	TransferID   string `json:"transferId"`
	FromWalletID string `json:"fromWalletId"`
	ToWalletID   string `json:"toWalletId"`
	Amount       int64  `json:"amount"`
	Status       string `json:"status"`
	ErrorReason  string `json:"errorReason,omitempty"`
	CreatedAt    string `json:"createdAt"`
	ProcessedAt  string `json:"processedAt,omitempty"`
}

// GetTransferResponse represents the API response for getting a transfer
type GetTransferResponse struct {
	TransferID   string `json:"transferId"`
	FromWalletID string `json:"fromWalletId"`
	ToWalletID   string `json:"toWalletId"`
	Amount       int64  `json:"amount"`
	Status       string `json:"status"`
	ErrorReason  string `json:"errorReason,omitempty"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details,omitempty"`
}

package models

// CreateTransferRequest represents the API request for creating a transfer
type CreateTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"` // Amount in cents (minor units)
}

// Validate validates the create transfer request
func (r *CreateTransferRequest) Validate() error {
	if r.IdempotencyKey == "" {
		return &ValidationError{Field: "idempotencyKey", Message: "idempotency key is required"}
	}
	if r.FromWalletID == "" {
		return &ValidationError{Field: "fromWalletId", Message: "from wallet ID is required"}
	}
	if r.ToWalletID == "" {
		return &ValidationError{Field: "toWalletId", Message: "to wallet ID is required"}
	}
	if r.FromWalletID == r.ToWalletID {
		return &ValidationError{Field: "toWalletId", Message: "cannot transfer to the same wallet"}
	}
	if r.Amount <= 0 {
		return &ValidationError{Field: "amount", Message: "amount must be positive"}
	}
	return nil
}

// TransferResponse represents the API response for a transfer
type TransferResponse struct {
	ID           string `json:"id"`
	FromWalletID string `json:"fromWalletId"`
	ToWalletID   string `json:"toWalletId"`
	Amount       int64  `json:"amount"` // Amount in cents (minor units)
	Status       string `json:"status"`
	CreatedAt    string `json:"createdAt"`
}

// ErrorResponse represents an API error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

// ValidationError represents a validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

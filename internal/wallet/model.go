package wallet

import "time"

// AuditLog tracks who created and last modified a record.
type AuditLog struct {
	CreatedBy string    `json:"created_by"`
	UpdatedBy string    `json:"updated_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Wallet represents a user wallet.
type Wallet struct {
	ID         string   `json:"id"`
	OwnerName  string   `json:"owner_name"`
	OwnerEmail string   `json:"owner_email"`
	Balance    float64  `json:"balance"`
	AuditLog   AuditLog `json:"audit_log"`
}

// CreateWalletRequest is the request body for creating a wallet.
type CreateWalletRequest struct {
	OwnerName      string  `json:"owner_name" validate:"required"`
	OwnerEmail     string  `json:"owner_email" validate:"required,email"`
	InitialBalance float64 `json:"initial_balance" validate:"gte=0"`
}

// BalanceResponse is the response for the get-balance endpoint.
type BalanceResponse struct {
	WalletID string  `json:"wallet_id"`
	Balance  float64 `json:"balance"`
}

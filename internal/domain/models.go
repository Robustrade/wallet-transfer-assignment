package domain

import "time"

// TransferStatus represents the state of a transfer.
type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "PENDING"
	TransferStatusProcessed TransferStatus = "PROCESSED"
	TransferStatusFailed    TransferStatus = "FAILED"
)

// EntryType represents a ledger entry direction.
type EntryType string

const (
	EntryTypeDebit  EntryType = "DEBIT"
	EntryTypeCredit EntryType = "CREDIT"
)

// Wallet represents a user wallet with a stored balance.
type Wallet struct {
	ID        string    `json:"id"`
	Balance   int64     `json:"balance"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Transfer represents a wallet-to-wallet transfer.
type Transfer struct {
	ID             string         `json:"id"`
	FromWalletID   string         `json:"fromWalletId"`
	ToWalletID     string         `json:"toWalletId"`
	Amount         int64          `json:"amount"`
	Status         TransferStatus `json:"status"`
	IdempotencyKey string         `json:"idempotencyKey"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

// LedgerEntry represents a single entry in the double-entry ledger.
type LedgerEntry struct {
	ID         string    `json:"id"`
	WalletID   string    `json:"walletId"`
	TransferID string    `json:"transferId"`
	EntryType  EntryType `json:"entryType"`
	Amount     int64     `json:"amount"`
	CreatedAt  time.Time `json:"createdAt"`
}

// IdempotencyRecord stores a cached response for replay.
type IdempotencyRecord struct {
	IdempotencyKey string    `json:"idempotencyKey"`
	TransferID     string    `json:"transferId"`
	ResponseStatus int       `json:"responseStatus"`
	ResponseBody   string    `json:"responseBody"`
	CreatedAt      time.Time `json:"createdAt"`
}

// TransferRequest is the inbound API request for creating a transfer.
type TransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

// Validate checks request fields and returns a domain error if invalid.
func (r *TransferRequest) Validate() error {
	if r.IdempotencyKey == "" {
		return ErrInvalidRequest
	}
	if r.FromWalletID == "" || r.ToWalletID == "" {
		return ErrInvalidRequest
	}
	if r.Amount <= 0 {
		return ErrInvalidAmount
	}
	if r.FromWalletID == r.ToWalletID {
		return ErrSameWallet
	}
	return nil
}

// TransferResponse is the outbound API response after a transfer.
type TransferResponse struct {
	ID             string         `json:"id"`
	FromWalletID   string         `json:"fromWalletId"`
	ToWalletID     string         `json:"toWalletId"`
	Amount         int64          `json:"amount"`
	Status         TransferStatus `json:"status"`
	IdempotencyKey string         `json:"idempotencyKey"`
}

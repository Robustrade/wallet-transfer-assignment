package models

// TransferStatus represents the current state of a money movement request.
type TransferStatus string

const (
	// TransferStatusPending means the request has been received but not yet processed.
	TransferStatusPending TransferStatus = "PENDING"
	// TransferStatusProcessed means the funds have been successfully moved and ledgered.
	TransferStatusProcessed TransferStatus = "PROCESSED"
	// TransferStatusFailed means the transfer was rejected (e.g., due to insufficient funds).
	TransferStatusFailed TransferStatus = "FAILED"
)

// Transfer represents a single wallet-to-wallet transaction.
// It includes the unique idempotency key to ensure exactly-once semantics.
type Transfer struct {
	ID             string         `gorm:"primaryKey;type:uuid" json:"id"`
	IdempotencyKey string         `gorm:"uniqueIndex;not null" json:"idempotency_key"`
	SourceWalletID string         `gorm:"not null;index" json:"fromWalletId"`
	DestWalletID   string         `gorm:"not null;index" json:"toWalletId"`
	Amount         int64          `gorm:"not null" json:"amount"` // Amount in the smallest currency unit (e.g., cents)
	Status         TransferStatus `gorm:"not null;default:'PENDING'" json:"status"`
	FailureReason  string         `json:"failure_reason,omitempty"`
	CreatedAt      int64          `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      int64          `gorm:"autoUpdateTime" json:"updated_at"`
}

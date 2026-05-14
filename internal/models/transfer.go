package models

import (
	"fmt"
	"time"
)

// Transfer represents a wallet transfer
type Transfer struct {
	TransferID   string    `gorm:"primaryKey;column:transfer_id" json:"transfer_id"`
	FromWalletID string    `gorm:"column:from_wallet_id;index:idx_transfers_from_wallet" json:"from_wallet_id"`
	ToWalletID   string    `gorm:"column:to_wallet_id;index:idx_transfers_to_wallet" json:"to_wallet_id"`
	Amount       int64     `gorm:"column:amount" json:"amount"`
	Status       string    `gorm:"column:status" json:"status"`
	CreatedAt    time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (Transfer) TableName() string {
	return "transfers"
}

// TransferStatus constants
const (
	TransferStatusPending   = "PENDING"
	TransferStatusProcessed = "PROCESSED"
	TransferStatusFailed    = "FAILED"
)

// Validate validates transfer input
func (t *Transfer) Validate() error {
	if t.Amount <= 0 {
		return fmt.Errorf("amount must be positive, got %d", t.Amount)
	}
	if t.FromWalletID == "" || t.ToWalletID == "" {
		return fmt.Errorf("both wallet IDs must be provided")
	}
	if t.FromWalletID == t.ToWalletID {
		return fmt.Errorf("cannot transfer to the same wallet")
	}
	return nil
}

// IsValidTransition checks if a state transition is valid
func IsValidTransition(currentStatus, newStatus string) bool {
	validTransitions := map[string][]string{
		TransferStatusPending:   {TransferStatusProcessed, TransferStatusFailed},
		TransferStatusProcessed: {},
		TransferStatusFailed:    {},
	}
	for _, valid := range validTransitions[currentStatus] {
		if valid == newStatus {
			return true
		}
	}
	return false
}

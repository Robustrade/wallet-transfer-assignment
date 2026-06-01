package wallet

import (
	"errors"
	"time"
)

// Status represents the lifecycle state of a wallet.
type Status string

const (
	StatusActive   Status = "ACTIVE"
	StatusInactive Status = "INACTIVE"
)

// Wallet holds the identity and metadata of a wallet.
//
// Design notes:
//   - Pure domain model — no storage tags, no DB imports.
//   - ID is domain-generated (prefixed UUID v7) before any DB call.
//     Format: "wallet_<uuidv7hex>", e.g. "wallet_01960f3aab127e4ceab123..."
//   - Currency is immutable and set once at creation; it is part of the wallet's identity.
//   - Balance is NOT stored here — it lives in the ledger domain.
type Wallet struct {
	ID        string
	OwnerID   string
	Currency  string    // immutable; set once at wallet creation
	Status    Status
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ValidateForCreate checks business rules that must hold before first persistence.
// ID is already set by the domain at construction time.
func (w *Wallet) ValidateForCreate() error {
	if w.ID == "" {
		return errors.New("wallet: id is required")
	}
	if w.OwnerID == "" {
		return errors.New("wallet: owner_id is required")
	}
	if w.Currency == "" {
		return errors.New("wallet: currency is required")
	}
	if w.Status != StatusActive && w.Status != StatusInactive {
		return errors.New("wallet: invalid status")
	}
	return nil
}

// Validate checks a fully-persisted wallet is internally consistent.
// Use for domain checks after loading from the DB (e.g. before authorising a transfer).
func (w *Wallet) Validate() error {
	return w.ValidateForCreate()
}

// IsActive returns true when the wallet can participate in transfers.
func (w *Wallet) IsActive() bool {
	return w.Status == StatusActive
}

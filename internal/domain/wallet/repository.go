package wallet

import "context"

// Repository defines the persistence contract for the wallet domain.
// Concrete implementations live in internal/platform/postgres.
// No DB driver is imported here.
type Repository interface {
	// GetByID returns a wallet by its unique ID.
	// Returns ErrNotFound if the wallet does not exist.
	GetByID(ctx context.Context, id string) (*Wallet, error)

	// ValidateWallets checks that all provided wallet IDs exist and are ACTIVE.
	// The active check is enforced in the SQL query itself — a wallet that exists
	// but is inactive is treated the same as one that does not exist.
	// Returns a descriptive error for the first ID that fails validation.
	// This is the only cross-domain wallet check the transfer domain needs.
	ValidateWallets(ctx context.Context, ids []string) error

	// Create persists a new wallet.
	// Returns ErrDuplicate if a wallet with the same ID already exists.
	Create(ctx context.Context, w *Wallet) error

	// List returns all wallets for a given owner.
	List(ctx context.Context, ownerID string) ([]*Wallet, error)
}

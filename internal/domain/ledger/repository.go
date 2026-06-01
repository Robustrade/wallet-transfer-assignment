package ledger

import "context"

// LedgerRepository defines the persistence contract for ledger entries.
// Implementations live in internal/platform/postgres.
type LedgerRepository interface {
	// InsertDoubleEntry persists exactly two entries (debit + credit) atomically.
	// The caller is responsible for passing both entries in the same transaction.
	InsertDoubleEntry(ctx context.Context, debit *Entry, credit *Entry) error

	// GetByWalletID returns all ledger entries for a given wallet, newest first.
	GetByWalletID(ctx context.Context, walletID string) ([]*Entry, error)
}

// BalanceRepository defines the persistence contract for wallet balances.
// Balance reads and writes are always co-located with ledger writes (same transaction),
// which is why both repositories live in the same domain package.
type BalanceRepository interface {
	// GetBalance returns the current balance for a wallet.
	GetBalance(ctx context.Context, walletID string) (*Balance, error)

	// DebitAndCredit applies a debit to fromWalletID and a credit to toWalletID
	// within a single database transaction using SELECT FOR UPDATE row locking.
	// Returns ErrInsufficientBalance if the source balance would go negative.
	DebitAndCredit(ctx context.Context, fromWalletID, toWalletID string, amount int64) error
}

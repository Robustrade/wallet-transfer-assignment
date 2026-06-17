// Package domain holds the core entities, value types, state-transition rules,
// and typed errors for the wallet transfer service. It has no knowledge of
// HTTP or of the database driver -- it is pure business vocabulary.
package domain

// Amount is money in minor units (e.g. cents). Integers only -- never floats.
type Amount = int64

// TransferStatus is the lifecycle state of a transfer.
type TransferStatus string

const (
	StatusPending   TransferStatus = "PENDING"
	StatusProcessed TransferStatus = "PROCESSED"
	StatusFailed    TransferStatus = "FAILED"
)

// EntryType is the direction of a ledger entry relative to a wallet.
type EntryType string

const (
	EntryDebit  EntryType = "DEBIT"  // money leaves the wallet
	EntryCredit EntryType = "CREDIT" // money enters the wallet
)

// Wallet is a user account holding a balance.
type Wallet struct {
	ID       string
	Balance  Amount
	Currency string
}

// Transfer is a single wallet-to-wallet movement and its terminal state.
type Transfer struct {
	ID            string
	FromWalletID  string
	ToWalletID    string
	Amount        Amount
	Status        TransferStatus
	FailureReason string
}

// LedgerEntry is one half of a double-entry record for a transfer.
type LedgerEntry struct {
	TransferID string
	WalletID   string
	Type       EntryType
	Amount     Amount
}

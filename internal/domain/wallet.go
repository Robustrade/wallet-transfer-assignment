package domain

import "time"

// Wallet represents a monetary account that can send and receive funds.
//
// Balance is stored as an integer in the currency's smallest unit (e.g.
// cents), never as a float, to avoid rounding errors accumulating across
// many transfers.
type Wallet struct {
	ID        string
	Balance   int64
	Version   int64 // bumped on every balance change; useful for auditing/debugging, not used for locking (see repository.Tx.GetWalletForUpdate)
	CreatedAt time.Time
	UpdatedAt time.Time
}

package domain

import "time"

// Wallet is the canonical representation of an account that holds a
// balance. It deliberately contains no methods that mutate the balance —
// changing a balance always goes through the service layer so that the
// surrounding transaction, lock, and ledger entries are produced
// together.
//
// Field notes:
//
//   - ID is caller-supplied (e.g. "wallet_1"). The schema uses TEXT
//     rather than UUID so the assignment's example identifiers work
//     unchanged.
//
//   - Balance is in integer minor units (cents). Money is never stored
//     or computed as float — see AGENTS.md §S-2 for the rationale.
//
//   - CreatedAt / UpdatedAt are managed by the database with `DEFAULT
//     now()` plus an explicit UPDATE on every balance change. The
//     application never sets these manually.
type Wallet struct {
	ID        string
	Balance   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

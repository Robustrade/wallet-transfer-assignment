package domain

import (
	"time"

	"github.com/google/uuid"
)

// EntryType is the side of a double-entry ledger entry.
//
// Each successful Transfer produces exactly two LedgerEntry rows: one
// EntryDebit against the source wallet and one EntryCredit against the
// destination. The database enforces this with a UNIQUE (transfer_id,
// type) index, so a buggy code path that tried to insert two debits
// would be rejected with a unique-violation rather than silently
// producing an unbalanced ledger.
type EntryType string

const (
	// EntryDebit reduces the recorded balance on the wallet referenced
	// by LedgerEntry.WalletID.
	EntryDebit EntryType = "DEBIT"

	// EntryCredit increases the recorded balance on the wallet
	// referenced by LedgerEntry.WalletID.
	EntryCredit EntryType = "CREDIT"
)

// LedgerEntry is an append-only audit row recording one side of a
// transfer. The pair of entries (DEBIT + CREDIT) for the same
// TransferID always sums to zero, which is the invariant tests assert.
//
// Field notes:
//
//   - ID is auto-assigned by the database (BIGSERIAL). Callers ignore
//     this on insert.
//
//   - TransferID points at the parent transfer row. The FK is required
//     at the schema level so orphan ledger rows are impossible.
//
//   - WalletID identifies which wallet's balance this entry affects.
//
//   - Type is EntryDebit or EntryCredit.
//
//   - Amount is positive minor units. The sign of the effect on the
//     wallet's balance is carried by Type, not by Amount; this matches
//     standard double-entry bookkeeping practice and avoids ambiguity
//     about whether negative amounts are reversals or just debits.
type LedgerEntry struct {
	ID         int64
	TransferID uuid.UUID
	WalletID   string
	Type       EntryType
	Amount     int64
	CreatedAt  time.Time
}

package model

import (
	"time"

	"github.com/google/uuid"

	"github.com/Robustrade/wallet-transfer-assignment/common/util/money"
)

// EntryType is the kind of ledger entry: DEBIT subtracts from a wallet,
// CREDIT adds to a wallet.
type EntryType string

// Allowed values for EntryType.
const (
	EntryDebit  EntryType = "DEBIT"
	EntryCredit EntryType = "CREDIT"
)

// LedgerEntry is one line of the immutable double-entry ledger.
// Every transfer produces exactly two entries: one DEBIT and one CREDIT.
type LedgerEntry struct {
	ID         int64
	TransferID uuid.UUID
	WalletID   string
	EntryType  EntryType
	Amount     money.Money
	CreatedAt  time.Time
}

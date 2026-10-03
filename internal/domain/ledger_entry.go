package domain

// EntryType says whether a ledger entry takes money out of a wallet or puts
// money in.
type EntryType string

const (
	EntryDebit  EntryType = "DEBIT"
	EntryCredit EntryType = "CREDIT"
)

// LedgerEntry records one side of a transfer on one wallet.
type LedgerEntry struct {
	TransferID string
	WalletID   string
	Type       EntryType
	Amount     int64
}

// LedgerEntries returns the two entries of a processed transfer: a DEBIT on
// the source wallet and a CREDIT on the destination wallet, both for Amount.
func (t Transfer) LedgerEntries() []LedgerEntry {
	return []LedgerEntry{
		{TransferID: t.ID, WalletID: t.FromWalletID, Type: EntryDebit, Amount: t.Amount},
		{TransferID: t.ID, WalletID: t.ToWalletID, Type: EntryCredit, Amount: t.Amount},
	}
}

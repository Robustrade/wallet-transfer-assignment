package domain

type EntryType string

const (
	EntryDebit  EntryType = "DEBIT"
	EntryCredit EntryType = "CREDIT"
)

// LedgerEntry is an immutable accounting record associated with a transfer.
type LedgerEntry struct {
	ID         string
	TransferID string
	WalletID   string
	Type       EntryType
	Amount     Amount
}

func (e LedgerEntry) Validate() error {
	if e.ID == "" || e.TransferID == "" || e.WalletID == "" || e.Amount <= 0 {
		return ErrInvalidLedgerEntry
	}
	if e.Type != EntryDebit && e.Type != EntryCredit {
		return ErrInvalidLedgerEntry
	}
	return nil
}

// ValidateLedgerPair checks that debit and credit entries balance one transfer.
func ValidateLedgerPair(debit, credit LedgerEntry) error {
	if err := debit.Validate(); err != nil {
		return err
	}
	if err := credit.Validate(); err != nil {
		return err
	}
	if debit.Type != EntryDebit || credit.Type != EntryCredit ||
		debit.TransferID != credit.TransferID || debit.WalletID == credit.WalletID ||
		debit.Amount != credit.Amount {
		return ErrUnbalancedLedger
	}
	return nil
}

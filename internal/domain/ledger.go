package domain

type LedgerEntryID string
type LedgerEntryType string

const (
	LedgerEntryTypeDebit  LedgerEntryType = "DEBIT"
	LedgerEntryTypeCredit LedgerEntryType = "CREDIT"
)

type LedgerEntry struct {
	ID         LedgerEntryID
	WalletID   WalletID
	TransferID TransferID
	Type       LedgerEntryType
	Amount     int64
}

func NewCreditEntry(walletID WalletID, transferID TransferID, amount int64) (*LedgerEntry, error) {

	if amount <= 0 {
		return nil, ErrInvalidAmount
	}
	ledgerEntry := &LedgerEntry{
		WalletID:   walletID,
		TransferID: transferID,
		Amount:     amount,
		Type:       LedgerEntryTypeCredit,
	}

	return ledgerEntry, nil

}
func NewDebitEntry(walletID WalletID, transferID TransferID, amount int64) (*LedgerEntry, error) {
	if amount <= 0 {
		return nil, ErrInvalidAmount
	}
	ledgerEntry := &LedgerEntry{
		WalletID:   walletID,
		TransferID: transferID,
		Amount:     amount,
		Type:       LedgerEntryTypeDebit,
	}

	return ledgerEntry, nil

}

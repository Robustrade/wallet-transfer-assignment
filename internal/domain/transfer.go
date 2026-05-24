package domain

type Transfer struct {
	ID           TransferID
	FromWalletID WalletID
	ToWalletID   WalletID
	Amount       int64
	Status       TransferStatus
}

type TransferID string
type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "PENDING"
	TransferStatusProcessed TransferStatus = "PROCESSED"
	TransferStatusFailed    TransferStatus = "FAILED"
)

func (t *Transfer) Validate() error {
	if t.Amount <= 0 {
		return ErrInvalidAmount
	}
	if t.FromWalletID == t.ToWalletID {
		return ErrSameWalletTransfer
	}
	return nil

}
func (t *Transfer) MarkProcessed() error {
	if t.Status != TransferStatusPending {
		return ErrInvalidTransferState

	}
	t.Status = TransferStatusProcessed
	return nil
}
func (t *Transfer) MarkFailed() error {
	if t.Status != TransferStatusPending {
		return ErrInvalidTransferState

	}
	t.Status = TransferStatusFailed
	return nil
}

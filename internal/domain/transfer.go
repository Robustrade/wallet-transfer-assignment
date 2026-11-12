package domain

import "fmt"

const maxIdentifierLength = 255

func NewTransfer(id string, req TransferRequest) (*Transfer, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	return &Transfer{
		ID:             id,
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		Status:         StatusPending,
	}, nil
}

func (r TransferRequest) validate() error {
	switch {
	case r.IdempotencyKey == "":
		return newValidationError("idempotencyKey", "is required")
	case len(r.IdempotencyKey) > maxIdentifierLength:
		return newValidationError("idempotencyKey", fmt.Sprintf("must be at most %d characters", maxIdentifierLength))
	case r.FromWalletID == "":
		return newValidationError("fromWalletId", "is required")
	case r.ToWalletID == "":
		return newValidationError("toWalletId", "is required")
	case r.FromWalletID == r.ToWalletID:
		return newValidationError("toWalletId", "must be different from fromWalletId")
	case r.Amount <= 0:
		return newValidationError("amount", "must be greater than zero")
	}
	return nil
}

func (t *Transfer) MarkProcessed() error {
	if err := t.ensurePending(StatusProcessed); err != nil {
		return err
	}
	t.Status = StatusProcessed
	return nil
}

func (t *Transfer) MarkFailed(reason FailureReason) error {
	if reason == "" {
		return newValidationError("failureReason", "is required")
	}
	if err := t.ensurePending(StatusFailed); err != nil {
		return err
	}
	t.Status = StatusFailed
	t.FailureReason = reason
	return nil
}

func (t *Transfer) LedgerEntries() ([]LedgerEntry, error) {
	if t.Status != StatusProcessed {
		return nil, ErrTransferNotProcessed
	}
	return []LedgerEntry{
		{TransferID: t.ID, WalletID: t.FromWalletID, Type: EntryDebit, Amount: t.Amount},
		{TransferID: t.ID, WalletID: t.ToWalletID, Type: EntryCredit, Amount: t.Amount},
	}, nil
}

func (t *Transfer) ensurePending(next TransferStatus) error {
	if t.Status != StatusPending {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidStatusTransition, t.Status, next)
	}
	return nil
}

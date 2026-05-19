package service

import (
	"context"
	"errors"

	"github.com/avinilcodes/wallet-transfer-assignment/internal/domain"
	"github.com/avinilcodes/wallet-transfer-assignment/internal/repository"
)

// ErrNotImplemented is retained for handler tests that stub the service.
var ErrNotImplemented = errors.New("not implemented")

// CreateTransferInput is the service-layer input for POST /transfers.
type CreateTransferInput struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

// CreateTransferResult is the service-layer output for a completed or replayed transfer.
type CreateTransferResult struct {
	TransferID string                `json:"transferId"`
	State      domain.TransferState  `json:"state"`
	Amount     int64                 `json:"amount"`
	Currency   string                `json:"currency"`
}

// TransferService orchestrates wallet transfers.
type TransferService interface {
	CreateTransfer(ctx context.Context, input CreateTransferInput) (*CreateTransferResult, error)
}

// TransferServiceImpl implements TransferService.
type TransferServiceImpl struct {
	store repository.Store
}

// NewTransferService wires dependencies for transfer workflows.
func NewTransferService(store repository.Store) *TransferServiceImpl {
	return &TransferServiceImpl{store: store}
}

// CreateTransfer executes the transfer workflow atomically.
func (s *TransferServiceImpl) CreateTransfer(ctx context.Context, input CreateTransferInput) (*CreateTransferResult, error) {
	if result, found, err := s.lookupIdempotentResult(ctx, input); err != nil {
		return nil, err
	} else if found {
		return result, nil
	}

	if err := validateCreateTransferInput(input); err != nil {
		return nil, err
	}

	transferID, err := newTransferID()
	if err != nil {
		return nil, err
	}

	var result *CreateTransferResult
	err = s.store.Tx().WithTransaction(ctx, func(txCtx context.Context) error {
		var execErr error
		result, execErr = s.executeTransfer(txCtx, input, transferID)
		return execErr
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

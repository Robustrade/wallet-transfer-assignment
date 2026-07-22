// Package service holds the transfer business logic: saving a request safely and
// processing one transfer. It controls the transaction boundaries through the
// repository interfaces and never writes SQL directly.
package service

import (
	"context"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/repository"
)

// TransferService is what the handler calls: validate and save a request, and read it back.
type TransferService struct {
	store repository.Store
	log   *zap.Logger
	rec   Recorder
}

func NewTransferService(store repository.Store, log *zap.Logger, rec Recorder) *TransferService {
	return &TransferService{store: store, log: log, rec: rec}
}

// CreateTransfer validates the request and saves a PENDING transfer. The bool is
// true only when a new transfer was created; if the same idempotency key was seen
// before, it returns the original transfer with false.
func (s *TransferService) CreateTransfer(ctx context.Context, p domain.NewTransferParams) (domain.Transfer, bool, error) {
	if err := p.Validate(); err != nil {
		return domain.Transfer{}, false, err
	}
	transfer, created, err := s.store.EnqueueTransfer(ctx, p)
	if err == nil && created {
		s.rec.TransferEnqueued()
	}
	return transfer, created, err
}

// GetTransfer returns the current state of a transfer (for result polling).
func (s *TransferService) GetTransfer(ctx context.Context, id uuid.UUID) (domain.Transfer, error) {
	return s.store.GetTransferByID(ctx, id)
}

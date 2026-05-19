package memory

import (
	"context"
	"time"

	"github.com/avinilcodes/wallet-transfer-assignment/internal/domain"
)

type transferRepo struct {
	s *Store
}

func (r *transferRepo) Create(_ context.Context, transfer *domain.Transfer) error {
	r.s.mustBeInTransaction()

	if _, exists := r.s.transfers[transfer.ID]; exists {
		return domain.ErrInvalidTransfer
	}
	now := time.Now().UTC()
	copy := *transfer
	copy.CreatedAt = now
	copy.UpdatedAt = now
	r.s.transfers[transfer.ID] = &copy
	transfer.CreatedAt = copy.CreatedAt
	transfer.UpdatedAt = copy.UpdatedAt
	return nil
}

func (r *transferRepo) GetByID(_ context.Context, transferID string) (*domain.Transfer, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	t, ok := r.s.transfers[transferID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	copy := *t
	return &copy, nil
}

func (r *transferRepo) UpdateState(_ context.Context, transferID string, state domain.TransferState) error {
	r.s.mustBeInTransaction()

	t, ok := r.s.transfers[transferID]
	if !ok {
		return domain.ErrNotFound
	}
	t.State = state
	t.UpdatedAt = time.Now().UTC()
	return nil
}

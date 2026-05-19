package memory

import (
	"context"

	"github.com/avinilcodes/wallet-transfer-assignment/internal/domain"
)

type idempotencyRepo struct {
	s *Store
}

func (r *idempotencyRepo) GetByKey(_ context.Context, key string) (*domain.IdempotencyRecord, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	record, ok := r.s.idempotency[key]
	if !ok {
		return nil, domain.ErrNotFound
	}
	copy := *record
	return &copy, nil
}

func (r *idempotencyRepo) Save(_ context.Context, record *domain.IdempotencyRecord) error {
	r.s.mustBeInTransaction()

	if _, exists := r.s.idempotency[record.Key]; exists {
		return domain.ErrIdempotencyConflict
	}
	copy := *record
	r.s.idempotency[record.Key] = &copy
	return nil
}

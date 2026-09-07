package memory

import (
	"context"
	"time"

	"Wallet_Transfer_Service/internal/domain"
)

type idempotencyRepo Store

func (r *idempotencyRepo) Claim(ctx context.Context, key, requestHash string) (bool, *domain.IdempotencyRecord, error) {
	defer (*Store)(r).guard()()

	if existing, ok := r.idempotency[key]; ok {
		cp := *existing
		return false, &cp, nil
	}
	now := time.Now().UTC()
	rec := &domain.IdempotencyRecord{
		IdempotencyKey: key,
		RequestHash:    requestHash,
		Status:         domain.IdempotencyInProgress,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	r.idempotency[key] = rec
	cp := *rec
	return true, &cp, nil
}

func (r *idempotencyRepo) Complete(ctx context.Context, key, transferID string, responseStatus int, responseBody []byte) error {
	defer (*Store)(r).guard()()
	rec, ok := r.idempotency[key]
	if !ok {
		return domain.ErrTransferNotFound
	}
	rec.Status = domain.IdempotencyCompleted
	rec.TransferID = transferID
	rec.ResponseStatus = responseStatus
	rec.ResponseBody = responseBody
	rec.UpdatedAt = time.Now().UTC()
	return nil
}

func (r *idempotencyRepo) Get(ctx context.Context, key string) (*domain.IdempotencyRecord, error) {
	defer (*Store)(r).guard()()
	rec, ok := r.idempotency[key]
	if !ok {
		return nil, nil
	}
	cp := *rec
	return &cp, nil
}

package memory

import (
	"context"

	"Wallet_Transfer_Service/internal/domain"
)

type transferRepo Store

func (r *transferRepo) Create(ctx context.Context, t *domain.Transfer) error {
	defer (*Store)(r).guard()()
	cp := *t
	r.transfers[t.ID] = &cp
	return nil
}

func (r *transferRepo) UpdateState(ctx context.Context, t *domain.Transfer) error {
	defer (*Store)(r).guard()()
	if _, ok := r.transfers[t.ID]; !ok {
		return domain.ErrTransferNotFound
	}
	cp := *t
	r.transfers[t.ID] = &cp
	return nil
}

func (r *transferRepo) Get(ctx context.Context, id string) (*domain.Transfer, error) {
	defer (*Store)(r).guard()()
	t, ok := r.transfers[id]
	if !ok {
		return nil, domain.ErrTransferNotFound
	}
	cp := *t
	return &cp, nil
}

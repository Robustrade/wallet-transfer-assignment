package memory

import (
	"context"
	"time"

	"Wallet_Transfer_Service/internal/domain"
)

type walletRepo Store

func (r *walletRepo) Create(ctx context.Context, w *domain.Wallet) error {
	defer (*Store)(r).guard()()
	if _, exists := r.wallets[w.ID]; exists {
		return domain.ErrWalletExists
	}
	cp := *w
	r.wallets[w.ID] = &cp
	return nil
}

func (r *walletRepo) Get(ctx context.Context, id string) (*domain.Wallet, error) {
	defer (*Store)(r).guard()()
	return r.getLocked(id)
}

func (r *walletRepo) getLocked(id string) (*domain.Wallet, error) {
	w, ok := r.wallets[id]
	if !ok {
		return nil, domain.ErrWalletNotFound
	}
	cp := *w
	return &cp, nil
}

func (r *walletRepo) GetForUpdate(ctx context.Context, id string) (*domain.Wallet, error) {
	defer (*Store)(r).guard()()
	return r.getLocked(id)
}

func (r *walletRepo) UpdateBalance(ctx context.Context, id string, newBalance int64) error {
	defer (*Store)(r).guard()()
	w, ok := r.wallets[id]
	if !ok {
		return domain.ErrWalletNotFound
	}
	w.Balance = newBalance
	w.UpdatedAt = time.Now().UTC()
	return nil
}

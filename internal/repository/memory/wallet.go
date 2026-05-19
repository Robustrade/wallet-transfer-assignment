package memory

import (
	"context"

	"github.com/avinilcodes/wallet-transfer-assignment/internal/domain"
)

type walletRepo struct {
	s *Store
}

func (r *walletRepo) GetByID(_ context.Context, walletID string) (*domain.Wallet, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	w, ok := r.s.wallets[walletID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	copy := *w
	return &copy, nil
}

func (r *walletRepo) LockByID(_ context.Context, walletID string) (*domain.Wallet, error) {
	r.s.mustBeInTransaction()

	w, ok := r.s.wallets[walletID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	copy := *w
	return &copy, nil
}

func (r *walletRepo) UpdateBalance(_ context.Context, wallet *domain.Wallet) error {
	r.s.mustBeInTransaction()

	if _, ok := r.s.wallets[wallet.ID]; !ok {
		return domain.ErrNotFound
	}
	if wallet.Balance < 0 {
		return domain.ErrInsufficientFunds
	}
	copy := *wallet
	r.s.wallets[wallet.ID] = &copy
	return nil
}

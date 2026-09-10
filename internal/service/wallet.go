package service

import (
	"context"
	"time"

	"github.com/ankurnitt1311/wallet-transfer-assignment/internal/domain"
	"github.com/ankurnitt1311/wallet-transfer-assignment/internal/repository"
)

func (s *Service) CreateWallet(ctx context.Context, id string, openingBalance int64) (domain.Wallet, error) {
	wallet, err := domain.NewWallet(id, openingBalance, time.Now())
	if err != nil {
		return domain.Wallet{}, err
	}
	err = s.store.WithTx(ctx, func(tx *repository.Tx) error {
		return tx.InsertWallet(ctx, wallet)
	})
	if err != nil {
		return domain.Wallet{}, err
	}
	return wallet, nil
}

func (s *Service) GetWallet(ctx context.Context, id string) (domain.Wallet, error) {
	if id == "" {
		return domain.Wallet{}, domain.ErrMissingWalletID
	}
	return s.store.GetWallet(ctx, id)
}

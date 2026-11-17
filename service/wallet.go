package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/Robustrade/wallet-transfer-assignment/dto"
	"github.com/Robustrade/wallet-transfer-assignment/model"
	"github.com/Robustrade/wallet-transfer-assignment/repository"
)

// CreateWallet inserts a wallet. Duplicate id → ErrWalletAlreadyExists.
func (s *Service) CreateWallet(ctx context.Context, id string, balance int64) (dto.WalletView, error) {
	w, err := model.NewWallet(id, balance)
	if err != nil {
		return dto.WalletView{}, err
	}

	txCtx, tx, err := repository.BeginTx(ctx, s.pool)
	if err != nil {
		return dto.WalletView{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := s.wallets.Create(txCtx, w); err != nil {
		if errors.Is(err, model.ErrConflict) {
			return dto.WalletView{}, ErrWalletAlreadyExists
		}
		return dto.WalletView{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return dto.WalletView{}, fmt.Errorf("commit: %w", err)
	}
	return dto.NewWalletView(w), nil
}

// GetWallet returns the stored wallet or ErrWalletNotFound.
func (s *Service) GetWallet(ctx context.Context, id string) (dto.WalletView, error) {
	w, err := s.wallets.GetByID(ctx, id)
	if errors.Is(err, model.ErrNotFound) {
		return dto.WalletView{}, ErrWalletNotFound
	}
	if err != nil {
		return dto.WalletView{}, err
	}
	return dto.NewWalletView(w), nil
}

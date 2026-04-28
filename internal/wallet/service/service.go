package service

import (
	"context"

	"wallet-transfer/internal/wallet"
	walletRepo "wallet-transfer/internal/wallet/repository"
	"wallet-transfer/internal/wallet/static"
	"wallet-transfer/pkg/apperror"
	"wallet-transfer/pkg/database"
	"wallet-transfer/pkg/helper"
	"wallet-transfer/pkg/logger"
	"wallet-transfer/pkg/validate"
)

// WalletService defines business operations on wallets.
type WalletService interface {
	CreateWallet(ctx context.Context, req *wallet.CreateWalletRequest) (*wallet.Wallet, error)
	GetWallet(ctx context.Context, id string) (*wallet.Wallet, error)
	GetBalance(ctx context.Context, id string) (*wallet.BalanceResponse, error)
	GetAllWallets(ctx context.Context) ([]*wallet.Wallet, error)
}

type walletService struct {
	repo walletRepo.WalletRepository
	pool database.DBTX
}

// NewWalletService creates a new WalletService.
func NewWalletService(pool database.DBTX, repo walletRepo.WalletRepository) WalletService {
	return &walletService{repo: repo, pool: pool}
}

func (s *walletService) CreateWallet(ctx context.Context, req *wallet.CreateWalletRequest) (*wallet.Wallet, error) {
	const fn = "walletService.CreateWallet"

	if errStr := validate.Validate(req); errStr != "" {
		return nil, apperror.BadRequest(errStr)
	}

	w := &wallet.Wallet{
		ID:         helper.GenerateID("WAL"),
		OwnerName:  req.OwnerName,
		OwnerEmail: req.OwnerEmail,
		Balance:    req.InitialBalance,
		AuditLog: wallet.AuditLog{
			CreatedBy: "system",
			UpdatedBy: "system",
		},
	}
	if err := s.repo.Create(ctx, s.pool, w); err != nil {
		logger.Error(fn, "create failed", err, map[string]interface{}{"owner_email": req.OwnerEmail})
		return nil, apperror.InternalServerError("failed to create wallet")
	}

	logger.Info(fn, "wallet created", map[string]interface{}{"wallet_id": w.ID})
	return w, nil
}

func (s *walletService) GetWallet(ctx context.Context, id string) (*wallet.Wallet, error) {
	const fn = "walletService.GetWallet"

	w, err := s.repo.GetByID(ctx, s.pool, id)
	if err != nil {
		logger.Error(fn, "query failed", err, map[string]interface{}{"wallet_id": id})
		return nil, apperror.InternalServerError("failed to get wallet")
	}
	if w == nil {
		return nil, apperror.NotFound(static.WalletNotFound)
	}
	return w, nil
}

func (s *walletService) GetBalance(ctx context.Context, id string) (*wallet.BalanceResponse, error) {
	w, err := s.GetWallet(ctx, id)
	if err != nil {
		return nil, err
	}
	return &wallet.BalanceResponse{WalletID: w.ID, Balance: w.Balance}, nil
}

func (s *walletService) GetAllWallets(ctx context.Context) ([]*wallet.Wallet, error) {
	const fn = "walletService.GetAllWallets"

	wallets, err := s.repo.GetAll(ctx, s.pool)
	if err != nil {
		logger.Error(fn, "query all failed", err, nil)
		return nil, apperror.InternalServerError("failed to get wallets")
	}
	return wallets, nil
}

package controller

import (
	"context"
	"wallet-transfer-assigment/models"

	"go.uber.org/zap"
)

type walletController interface {
	GetWalletsDetails(ctx context.Context) ([]models.Wallet, error)
	GetWalletById(ctx context.Context, wallet_id string) (*models.Wallet, error)
	GetWalletStatementByWalletId(ctx context.Context, wallet_id string) ([]models.Statement, error)
	CreateWallet(ctx context.Context, request models.WalletRequest) (*models.Wallet, error)
}

func (ctr Controller) CreateWallet(ctx context.Context, request models.WalletRequest) (*models.Wallet, error) {
	wallet, err := ctr.db.CreateWallet(ctx, request.Balance)
	if err != nil {
		ctr.log.Error("unable to create wallet", zap.Error(err))
		return nil, err
	}
	walletResponse := ctr.maper.ToWalletReesponse(*wallet)
	return &walletResponse, nil
}

func (ctr Controller) GetWalletStatementByWalletId(ctx context.Context, wallet_id string) ([]models.Statement, error) {
	statements, err := ctr.db.GetStatementsByWalletId(ctx, wallet_id)
	if err != nil {
		ctr.log.Error("unable to fetch statement", zap.String("Wallet_id", wallet_id), zap.Error(err))
		return nil, err
	}
	walletStatements := ctr.maper.ToWalletStatements(statements)
	return walletStatements, nil
}

func (ctr Controller) GetWalletsDetails(ctx context.Context) ([]models.Wallet, error) {
	wallets_list, err := ctr.db.GetWallets(ctx)
	if err != nil {
		ctr.log.Error("unable to fetch wallets", zap.Error(err))
		return nil, err
	}
	ctr.log.Info("wallets fetched successfully:", zap.Any("len", len(wallets_list)))
	wallets := ctr.maper.ToWalletsResponse(wallets_list)
	return wallets, nil
}

func (ctr Controller) GetWalletById(ctx context.Context, wallet_id string) (*models.Wallet, error) {
	wallet_dbo, err := ctr.db.GetWalletById(ctx, wallet_id)
	if err != nil {
		ctr.log.Error("unable to fetch wallets", zap.Error(err))
		return nil, err
	}
	ctr.log.Info("wallets fetched successfully:")
	wallet_dto := ctr.maper.ToWalletResponse(*wallet_dbo)
	return &wallet_dto, nil
}

package controller

import (
	"context"
	"wallet-transfer-assigment/models"

	"go.uber.org/zap"
)

type walletController interface {
	GetWalletsDetails(ctx context.Context) ([]models.Wallet, error)
	GetWalletById(ctx context.Context, wallet_id string) (*models.Wallet, error)
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
	wallet_dto := ctr.maper.ToWalletResponse(wallet_dbo)
	return &wallet_dto, nil
}

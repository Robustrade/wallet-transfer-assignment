package datasource

import (
	"context"
	db "wallet-transfer-assigment/internal/db/generated"

	"go.uber.org/zap"
)

type walletDBer interface {
	GetWallets(ctx context.Context) ([]db.Wallet, error)
	GetWalletById(ctx context.Context, wallet_id string) (db.Wallet, error)
}

func (db DataSource) GetWallets(ctx context.Context) ([]db.Wallet, error) {
	wallets, err := db.Queries.GetWallets(ctx)
	return wallets, err
}

func (db DataSource) GetWalletById(ctx context.Context, wallet_id string) (db.Wallet, error) {
	wallet, err := db.Queries.GetWallet(ctx, wallet_id)
	if err != nil {
		db.log.Error(" unable to fetch wallet record", zap.Error(err))
	}
	return wallet, err
}

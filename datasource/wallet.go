package datasource

import (
	"context"
	db "wallet-transfer-assigment/internal/db/generated"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type walletDBer interface {
	GetWallets(ctx context.Context) ([]db.Wallet, error)
	GetWalletById(ctx context.Context, wallet_id string) (*db.Wallet, error)
	GetStatementsByWalletId(ctx context.Context, wallet_id string) ([]db.GetStatementByWalletIdRow, error)
	CreateWallet(ctx context.Context, Balance int64) (*db.CreateWalletRow, error)
}

func (ds DataSource) CreateWallet(ctx context.Context, Balance int64) (*db.CreateWalletRow, error) {
	walletDetails := db.CreateWalletParams{
		WalletID: uuid.New().String(),
		Balance:  Balance,
	}
	wallet, err := ds.Queries.CreateWallet(ctx, walletDetails)
	if err != nil {
		ds.log.Error(" unable to create wallet record", zap.Error(err))
		return nil, err
	}
	return &wallet, nil
}

func (ds DataSource) GetStatementsByWalletId(ctx context.Context, wallet_id string) ([]db.GetStatementByWalletIdRow, error) {
	statements, err := ds.Queries.GetStatementByWalletId(ctx, wallet_id)
	return statements, err
}
func (ds DataSource) GetWallets(ctx context.Context) ([]db.Wallet, error) {
	wallets, err := ds.Queries.GetWallets(ctx)
	return wallets, err
}

func (ds DataSource) GetWalletById(ctx context.Context, wallet_id string) (*db.Wallet, error) {
	wallet, err := ds.Queries.GetWallet(ctx, wallet_id)
	if err != nil {
		ds.log.Error(" unable to fetch wallet record", zap.Error(err))
		return nil, err
	}
	return &wallet, err
}

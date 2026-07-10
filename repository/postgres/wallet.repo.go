package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/loopinnovates/wallet-transfer-assignment/internal/domain"
	"github.com/loopinnovates/wallet-transfer-assignment/pkg/logger"
	"github.com/loopinnovates/wallet-transfer-assignment/repository/postgres/sqlcgen"
)

type WalletRepository struct {
	readQueries *sqlcgen.Queries
}

func NewWalletRepository(readDB *sql.DB, writeDB *sql.DB) *WalletRepository {
	return &WalletRepository{readQueries: sqlcgen.New(readDB)}
}

func (r *WalletRepository) GetByID(ctx context.Context, id string) (*domain.Wallet, error) {
	logger.Debug().Str("wallet_id", id).Msg("fetching wallet by id")
	return withRetry(ctx, func() (*domain.Wallet, error) {
		w, err := r.readQueries.GetWalletByID(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			logger.Warn().Str("wallet_id", id).Msg("wallet not found")
			return nil, domain.ErrWalletNotFound
		}
		if err != nil {
			logger.Warn().Err(err).Str("wallet_id", id).Msg("failed to fetch wallet by id")
			return nil, err
		}
		return toDomainWallet(w), nil
	})
}

func toDomainWallet(w sqlcgen.Wallet) *domain.Wallet {
	return &domain.Wallet{
		ID:        w.ID,
		OwnerName: w.OwnerName,
		Balance:   w.Balance,
		CreatedAt: w.CreatedAt,
		UpdatedAt: w.UpdatedAt,
	}
}

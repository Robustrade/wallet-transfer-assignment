package service

import (
	"github.com/Robustrade/wallet-transfer-assignment/repository"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Service owns transactions and orchestrates domain + repository calls.
type Service struct {
	pool      *pgxpool.Pool
	wallets   *repository.WalletRepository
	transfers *repository.TransferRepository
	ledger    *repository.LedgerRepository
	idem      *repository.IdempotencyRepository
}

func New(pool *pgxpool.Pool) *Service {
	return &Service{
		pool:      pool,
		wallets:   repository.NewWalletRepository(pool),
		transfers: repository.NewTransferRepository(pool),
		ledger:    repository.NewLedgerRepository(pool),
		idem:      repository.NewIdempotencyRepository(pool),
	}
}

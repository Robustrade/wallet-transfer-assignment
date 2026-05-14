package service

import (
	"wallet-transfer/internal/repository"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Service orchestrates business logic across the wallet and transfer domains.
type Service struct {
	pool         *pgxpool.Pool
	walletRepo   *repository.WalletRepository
	transferRepo *repository.TransferRepository
	ledgerRepo   *repository.LedgerRepository
}

// New wires a Service with its dependencies.
func New(
	pool *pgxpool.Pool,
	walletRepo *repository.WalletRepository,
	transferRepo *repository.TransferRepository,
	ledgerRepo *repository.LedgerRepository,
) *Service {
	return &Service{
		pool:         pool,
		walletRepo:   walletRepo,
		transferRepo: transferRepo,
		ledgerRepo:   ledgerRepo,
	}
}

// CreateWalletRequest holds the inputs for creating a wallet.
type CreateWalletRequest struct {
	ID             string
	InitialBalance int64
}

// CreateTransferRequest holds the inputs for initiating a transfer.
type CreateTransferRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

package postgres

import (
	"wallet-transfer-assignment/internal/repository"

	"gorm.io/gorm"
)

type repositoryImpl struct {
	walletRepo      repository.WalletRepository
	transferRepo    repository.TransferRepository
	ledgerRepo      repository.LedgerRepository
	idempotencyRepo repository.IdempotencyRepository
	repository.Transactor
}

// NewRepository creates a unified repository instance.
func NewRepository(db *gorm.DB) repository.Repository {
	return &repositoryImpl{
		walletRepo:      NewWalletRepository(db),
		transferRepo:    NewTransferRepository(db),
		ledgerRepo:      NewLedgerRepository(db),
		idempotencyRepo: NewIdempotencyRepository(db),
		Transactor:      NewTransactor(db),
	}
}

func (r *repositoryImpl) Wallets() repository.WalletRepository {
	return r.walletRepo
}

func (r *repositoryImpl) Transfers() repository.TransferRepository {
	return r.transferRepo
}

func (r *repositoryImpl) Ledgers() repository.LedgerRepository {
	return r.ledgerRepo
}

func (r *repositoryImpl) Idempotency() repository.IdempotencyRepository {
	return r.idempotencyRepo
}

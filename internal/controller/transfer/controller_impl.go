package transfer

import (
	"wallet-transfer-assignment/internal/models"
	"wallet-transfer-assignment/internal/repositories"
	"wallet-transfer-assignment/internal/services/cache"
)

type TransferControllerImpl struct {
	repo         repositories.Repository
	cacheService cache.CacheHelper
}

func NewTransferControllerImpl(repo repositories.Repository, cacheService cache.CacheHelper) TransferController {
	return &TransferControllerImpl{
		repo:         repo,
		cacheService: cacheService,
	}
}

// DoWalletTransfer orchestrates the transfer process by validating business rules
// and then delegating the atomic transaction to the repository.
func (b *TransferControllerImpl) DoWalletTransfer(transfer *models.Transfer) (*models.Transfer, error) {
	// 1. Pre-flight checks: Ensure IDs are valid, source != destination, and amount is within allowed limits.
	if err := validateDoWalletTransfer(transfer); err != nil {
		return nil, err
	}

	// 2. Transactional execution: Lock wallets, check balances, update ledger, and change state.
	return b.repo.ProcessTransfer(transfer)
}

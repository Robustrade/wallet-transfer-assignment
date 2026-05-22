package repositories

import "wallet-transfer-assignment/internal/models"

type Repository interface {
	ProcessTransfer(transfer *models.Transfer) (*models.Transfer, error)
	GetTransferByIdempotencyKey(key string) (*models.Transfer, error)
}

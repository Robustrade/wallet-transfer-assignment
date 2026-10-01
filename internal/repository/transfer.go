package repository

import (
	"gorm.io/gorm"

	"github.com/rajdeep-multipl/wallet-transfer-assignment/internal/models"
)

type TransferRepository struct{}

func NewTransferRepository() *TransferRepository { return &TransferRepository{} }

func (r *TransferRepository) Insert(tx *gorm.DB, t *models.Transfer) error {
	return tx.Create(t).Error
}

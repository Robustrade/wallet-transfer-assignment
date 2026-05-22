package transfer

import "wallet-transfer-assignment/internal/models"

type TransferController interface {
	DoWalletTransfer(transfer *models.Transfer) (*models.Transfer, error)
}

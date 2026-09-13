package mapper

import (
	db "wallet-transfer-assigment/internal/db/generated"
	"wallet-transfer-assigment/models"
	"wallet-transfer-assigment/utils"
)

type Mapp struct {
	log utils.Logger
}

type Mapper interface {
	ToWalletResponse(wallet db.Wallet) models.Wallet
	ToWalletsResponse(wallet []db.Wallet) []models.Wallet
	ToCreateTransferParams(transfer models.TransferRequest) db.CreateTransferParams
}

func NewMapper(log utils.Logger) Mapper {
	return Mapp{log: log}
}

func (m Mapp) ToWalletResponse(wallet db.Wallet) models.Wallet {
	return models.Wallet{
		WalletID:  wallet.WalletID,
		Balance:   wallet.Balance,
		CreatedAt: wallet.CreatedAt,
		UpdatedAt: wallet.UpdatedAt,
	}
}

func (m Mapp) ToWalletsResponse(wallet []db.Wallet) []models.Wallet {
	wallet_list_dto := make([]models.Wallet, 0, len(wallet))
	for _, val := range wallet {
		dto := m.ToWalletResponse(val)
		wallet_list_dto = append(wallet_list_dto, dto)
	}
	return wallet_list_dto
}

func (m Mapp) ToCreateTransferParams(transfer models.TransferRequest) db.CreateTransferParams {
	return db.CreateTransferParams{
		FromWalletID: transfer.FromWalletID,
		ToWalletID:   transfer.ToWalletID,
		Amount:       transfer.Amount,
		Status:       string(utils.PENDING),
	}
}

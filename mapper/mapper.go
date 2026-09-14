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
	ToWalletStatements(statements []db.GetStatementByWalletIdRow) []models.Statement
	ToResponseTransactions(transactions []db.Transfer) []models.Transfers
	ToWalletReesponse(wallet db.CreateWalletRow) models.Wallet
}

func NewMapper(log utils.Logger) Mapper {
	return Mapp{log: log}
}
func (m Mapp) ToWalletReesponse(wallet db.CreateWalletRow) models.Wallet {
	return models.Wallet{
		WalletID: wallet.WalletID,
		Balance:  wallet.Balance,
	}
}
func (m Mapp) ToResponseTransactions(transactions []db.Transfer) []models.Transfers {
	dtoList := make([]models.Transfers, 0, len(transactions))
	for _, transaction := range transactions {
		dtoList = append(dtoList, models.Transfers{
			TransactionID: transaction.TransactionID,
			FromWalletID:  transaction.FromWalletID,
			ToWalletID:    transaction.ToWalletID,
			Amount:        transaction.Amount,
			Status:        utils.TRANSFER_STATUS(transaction.Status),
			Comments:      transaction.Comments.String,
			CreatedAt:     transaction.CreatedAt,
			UpdatedAt:     transaction.UpdatedAt,
		})
	}
	return dtoList
}
func (m Mapp) ToWalletStatements(statements []db.GetStatementByWalletIdRow) []models.Statement {
	walletStatements := make([]models.Statement, 0, len(statements))
	for _, statement := range statements {
		walletStatements = append(walletStatements, models.Statement{
			CreatedAt:     statement.CreatedAt,
			TransactionID: statement.TransactionID,
			EntryID:       statement.EntryID,
			Amount:        statement.Amount,
			EntryType:     statement.EntryType,
			BalanceAfter:  statement.BalanceAfter,
			FromWalletID:  statement.FromWalletID,
			ToWalletID:    statement.ToWalletID,
			Status:        statement.Status,
			Comments:      statement.Comments.String,
		})
	}
	return walletStatements
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

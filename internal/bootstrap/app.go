package bootstrap

import (
	"wallet-transfer-assignment/internal/feature/wallet/handler"
	"wallet-transfer-assignment/internal/feature/wallet/repository"
	"wallet-transfer-assignment/internal/feature/wallet/service"

	"gorm.io/gorm"
)

type App struct {
	TransferHandler *handler.TransferHandler
}

func NewApp(db *gorm.DB) *App {
	// 1. Initialize repositories
	walletRepo := repository.NewWalletRepository()
	transferRepo := repository.NewTransferRepository()
	ledgerRepo := repository.NewLedgerRepository()
	idempotencyRepo := repository.NewIdempotencyRepository()

	// 2. Initialize transaction manager
	txManager := repository.NewTransactionManager(db)

	// 3. Initialize services
	transferService := service.NewTransferService(
		walletRepo,
		transferRepo,
		ledgerRepo,
		idempotencyRepo,
		txManager,
	)

	// 4. Initialize handlers
	transferHandler := handler.NewTransferHandler(transferService)

	return &App{
		TransferHandler: transferHandler,
	}
}

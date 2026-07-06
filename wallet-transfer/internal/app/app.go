package app

import (
	"database/sql"

	"wallet-transfer/internal/handler"
	"wallet-transfer/internal/repository"
	"wallet-transfer/internal/service"
)

// Application holds all the top-level handlers
type Application struct {
	transferHandler *handler.TransferHandler
	walletHandler   *handler.WalletHandler
}

// New wires up all the dependencies
func Initialize(db *sql.DB) *Application {
	// Repositories
	transferRepo := repository.NewTransferRepository(db)
	walletRepo := repository.NewWalletRepository(db)

	// Services
	transferService := service.NewTransferService(transferRepo)
	walletService := service.NewWalletService(walletRepo)

	return &Application{
		transferHandler: handler.NewTransferHandler(transferService),
		walletHandler:   handler.NewWalletHandler(walletService),
	}
}

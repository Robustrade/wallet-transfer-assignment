package tests

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-transfer-service/internal/repository"
	"wallet-transfer-service/internal/service"
)

func createService(
	pool *pgxpool.Pool,
) *service.TransferService {

	return service.NewTransferService(
		pool,
		repository.NewWalletRepository(),
		repository.NewTransferRepository(),
		repository.NewLedgerRepository(),
	)
}

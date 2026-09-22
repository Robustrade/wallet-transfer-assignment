package controller

import (
	"context"

	"github.com/wallet-transfer-assignment/wallet-transfer/internal/domain"
	"github.com/wallet-transfer-assignment/wallet-transfer/internal/service"
)

// TransferService defines only the transfer operations required by the
// transfer controller.
type TransferService interface {
	CreateTransfer(context.Context, service.CreateTransferInput) (*service.CreateTransferResult, error)
	GetTransfer(context.Context, string) (*service.CreateTransferResult, error)
}

// WalletService defines only the wallet operations required by the wallet
// controller.
type WalletService interface {
	CreateWallet(context.Context, service.CreateWalletInput) (*domain.Wallet, error)
	GetWallet(context.Context, string) (*domain.Wallet, error)
	ListTransferHistory(context.Context, string) ([]domain.Transfer, error)
}

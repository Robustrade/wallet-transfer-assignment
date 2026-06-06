package handler

import (
	"context"

	"github.com/google/uuid"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

// TransferSvc is the service interface the handler depends on.
// Keeping it here (not in the service package) avoids an import cycle and
// allows the handler to be tested with any conforming test double.
type TransferSvc interface {
	CreateWallet(ctx context.Context) (domain.Wallet, error)
	Execute(ctx context.Context, req service.TransferRequest) (service.TransferResponse, error)
	Magic(ctx context.Context, req service.MagicRequest) (service.TransferResponse, error)
	GetTransfer(ctx context.Context, id uuid.UUID) (domain.Transfer, error)
	GetWallet(ctx context.Context, id uuid.UUID) (domain.Wallet, error)
}

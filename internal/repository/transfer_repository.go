package repository

import (
	"context"

	"github.com/robustrade/wallet-transfer-assignment/internal/model"
)

type WalletRepository interface {
	Transfer(ctx context.Context, transfer *model.Transfer) error
}

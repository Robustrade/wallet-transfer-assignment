package repository

import (
	"context"

	"github.com/chaitanya-bhagat/wallet-transfer-assignment/internal/model"
)

type Repository interface {
	CreateTransfer(ctx context.Context, input model.CreateTransferRequest) (model.Transfer, error)
}

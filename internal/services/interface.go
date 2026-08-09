package services

import (
	"context"

	"github.com/chaitanya-bhagat/wallet-transfer-assignment/internal/model"
)

type Service interface {
	CreateTransfer(ctx context.Context, request model.CreateTransferRequest) (model.Transfer, error)
}

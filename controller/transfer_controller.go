package controller

import (
	"context"
	"wallet-transfer-assigment/models"

	"go.uber.org/zap"
)

type transactioner interface {
	Transfer(ctx context.Context, request models.TransferRequest) (*string, error)
}

func (ctr Controller) Transfer(ctx context.Context, request models.TransferRequest) (*string, error) {
	transferParam := ctr.maper.ToCreateTransferParams(request)
	transactionID, err := ctr.db.Transfer(ctx, transferParam, request.IdempotencyKey)
	if err != nil {
		ctr.log.Error(" transaction uncessfull", zap.Error(err))
		return nil, err
	}
	return transactionID, nil
}

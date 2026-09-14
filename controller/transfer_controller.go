package controller

import (
	"context"
	"wallet-transfer-assigment/models"

	"go.uber.org/zap"
)

type transactioner interface {
	Transfer(ctx context.Context, request models.TransferRequest) (*string, error)
	GetTransactions(ctx context.Context) ([]models.Transfers, error)
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

func (ctr Controller) GetTransactions(ctx context.Context) ([]models.Transfers, error) {
	transactions, err := ctr.db.GetTransactions(ctx)
	if err != nil {
		ctr.log.Error("unable to fetch statement", zap.Error(err))
		return nil, err
	}
	responseData := ctr.maper.ToResponseTransactions(transactions)
	return responseData, nil
}

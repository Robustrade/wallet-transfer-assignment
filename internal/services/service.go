package services

import (
	"context"
	"fmt"

	"github.com/chaitanya-bhagat/wallet-transfer-assignment/internal/model"
	"github.com/chaitanya-bhagat/wallet-transfer-assignment/internal/repository"
)

type transferService struct {
	repository repository.Repository
}

func NewTransferService(repo repository.Repository) Service {
	return &transferService{
		repository: repo,
	}
}

func (ts *transferService) CreateTransfer(ctx context.Context, request model.CreateTransferRequest) (model.Transfer, error) {
	if err := request.Validatation(); err != nil {
		return model.Transfer{}, err
	}

	transfer, err := ts.repository.CreateTransfer(ctx, request)
	if err != nil {
		return model.Transfer{}, fmt.Errorf("create transfer: %w", err)
	}
	return transfer, nil

}

package service

import (
	"context"
	"github.com/mshabab1296/wallet-transfer-assignment/internal/domain"
)

type TransferStore interface {
	CreateTransfer(context.Context, domain.CreateTransferRequest) (domain.Transfer, error)
}
type TransferService struct{ store TransferStore }

func NewTransferService(store TransferStore) *TransferService { return &TransferService{store: store} }
func (s *TransferService) CreateTransfer(ctx context.Context, req domain.CreateTransferRequest) (domain.Transfer, error) {
	if err := req.Validate(); err != nil {
		return domain.Transfer{}, err
	}
	return s.store.CreateTransfer(ctx, req)
}

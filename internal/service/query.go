package service

import (
	"context"

	"Wallet_Transfer_Service/internal/domain"
)

type TransferDetail struct {
	Transfer *domain.Transfer
	Entries  []*domain.LedgerEntry
}

func (s *TransferService) GetTransfer(ctx context.Context, id string) (*TransferDetail, error) {
	t, err := s.store.Transfers().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	entries, err := s.store.Ledger().ListByTransfer(ctx, id)
	if err != nil {
		return nil, err
	}
	return &TransferDetail{Transfer: t, Entries: entries}, nil
}

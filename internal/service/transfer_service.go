// Package service implements the transfer workflow: idempotency, the funds check,
// balance updates and ledger entries, all inside one database transaction.
package service

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/celinagupta/wallet-transfer-assignment/internal/domain"
	"github.com/celinagupta/wallet-transfer-assignment/internal/repository"
)

// Result is the outcome of CreateTransfer. Replayed is true when the transfer was
// created by an earlier request with the same idempotency key and is returned as-is.
type Result struct {
	Transfer domain.Transfer
	Replayed bool
}

type TransferService struct {
	tx     repository.TxManager
	logger *slog.Logger
	newID  func() uuid.UUID
}

func NewTransferService(tx repository.TxManager, logger *slog.Logger) *TransferService {
	return &TransferService{tx: tx, logger: logger, newID: uuid.New}
}

// CreateTransfer executes a transfer, or returns the stored result for a repeated
// idempotency key.
func (s *TransferService) CreateTransfer(ctx context.Context, req domain.TransferRequest) (Result, error) {
	return Result{}, nil // TODO: implement
}

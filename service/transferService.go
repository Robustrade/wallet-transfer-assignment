package service

import (
	"context"
	"database/sql"
	"fmt"

	"wallet-transfer/repository"
)

type TransferRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

type TransferResponse struct {
	Transfer *repository.Transfer
	IsReplay bool
}

type TransferService struct {
	repo *repository.WalletRepository
}

func NewTransferService(repo *repository.WalletRepository) *TransferService {
	return &TransferService{repo: repo}
}

func (s *TransferService) ProcessTransfer(ctx context.Context, req TransferRequest) (*TransferResponse, error) {
	if req.Amount <= 0 {
		return nil, fmt.Errorf("transfer amount must be positive")
	}

	var resp TransferResponse

	err := s.repo.ExecInTx(ctx, func(tx *sql.Tx) error {
		// 1. Idempotency Check
		existing, err := s.repo.GetTransferByIdempotencyKey(ctx, tx, req.IdempotencyKey)
		if err != nil {
			return err
		}
		if existing != nil {
			resp = TransferResponse{
				Transfer: existing,
				IsReplay: true,
			}
			return nil // Return early on duplicate idempotency key
		}

		// 2. Fetch & Lock Sender Wallet
		sender, err := s.repo.GetWalletForUpdate(ctx, tx, req.FromWalletID)
		if err != nil {
			return fmt.Errorf("sender wallet error: %w", err)
		}

		if sender.Balance < req.Amount {
			return repository.ErrInsufficientFund
		}

		// 3. Fetch & Lock Receiver Wallet
		receiver, err := s.repo.GetWalletForUpdate(ctx, tx, req.ToWalletID)
		if err != nil {
			return fmt.Errorf("receiver wallet error: %w", err)
		}

		// 4. Update Balances
		if err := s.repo.UpdateWalletBalance(ctx, tx, sender.ID, sender.Balance-req.Amount); err != nil {
			return err
		}
		if err := s.repo.UpdateWalletBalance(ctx, tx, receiver.ID, receiver.Balance+req.Amount); err != nil {
			return err
		}

		// 5. Create Transfer Record
		transferID := fmt.Sprintf("tx-%s", req.IdempotencyKey)
		transfer := &repository.Transfer{
			ID:             transferID,
			IdempotencyKey: req.IdempotencyKey,
			FromWalletID:   req.FromWalletID,
			ToWalletID:     req.ToWalletID,
			Amount:         req.Amount,
			Status:         "PROCESSED",
		}
		if err := s.repo.CreateTransfer(ctx, tx, transfer); err != nil {
			return err
		}

		// 6. Double-Entry Ledger Entries (DEBIT & CREDIT)
		if err := s.repo.CreateLedgerEntry(ctx, tx, transferID, req.FromWalletID, "DEBIT", req.Amount); err != nil {
			return err
		}
		if err := s.repo.CreateLedgerEntry(ctx, tx, transferID, req.ToWalletID, "CREDIT", req.Amount); err != nil {
			return err
		}

		resp = TransferResponse{
			Transfer: transfer,
			IsReplay: false,
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return &resp, nil
}
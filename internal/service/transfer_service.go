package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"wallet-transfer/internal/domain"
	postgresrepo "wallet-transfer/internal/repository/postgres"
)

var ErrInsufficientFunds = errors.New("insufficient funds")

type TransferRequest struct {
	IdempotencyKey string
	FromWalletID   uuid.UUID
	ToWalletID     uuid.UUID
	Amount         int64
}

type TransferService struct {
	repo *postgresrepo.Repository
}

func NewTransferService(repo *postgresrepo.Repository) *TransferService {
	return &TransferService{
		repo: repo,
	}
}

func (s *TransferService) CreateTransfer(
	ctx context.Context,
	req TransferRequest,
) (*domain.Transfer, error) {

	// 1. Validate request
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		return nil, errors.New("idempotency key is required")
	}

	if req.Amount <= 0 {
		return nil, errors.New("amount must be greater than zero")
	}

	if req.FromWalletID == req.ToWalletID {
		return nil, errors.New("source and destination wallets must be different")
	}

	// 2. Check whether this request was already processed.
	existingTransfer, err := s.repo.GetTransferByIdempotencyKey(
		ctx,
		req.IdempotencyKey,
	)
	if err != nil {
		return nil, err
	}

	if existingTransfer != nil {
		return existingTransfer, nil
	}

	// 3. Prepare a new PENDING transfer.
	transfer := &domain.Transfer{
		ID:           uuid.New(),
		FromWalletID: req.FromWalletID,
		ToWalletID:   req.ToWalletID,
		Amount:       req.Amount,
		Status:       domain.StatusPending,
		CreatedAt:    time.Now().UTC(),
	}

	var businessErr error

	// 4. Everything below happens in ONE database transaction.
	err = s.repo.WithTransaction(ctx, func(tx pgx.Tx) error {

		// Create PENDING transfer.
		if err := s.repo.CreateTransfer(
			ctx,
			tx,
			transfer,
		); err != nil {
			return err
		}

		// Claim the idempotency key.
		if err := s.repo.CreateIdempotencyRecord(
			ctx,
			tx,
			req.IdempotencyKey,
			transfer.ID,
		); err != nil {
			return err
		}

		// Lock wallets in deterministic order to reduce deadlock risk.
		firstWalletID := req.FromWalletID
		secondWalletID := req.ToWalletID

		if req.ToWalletID.String() < req.FromWalletID.String() {
			firstWalletID = req.ToWalletID
			secondWalletID = req.FromWalletID
		}

		firstWallet, err := s.repo.GetWalletForUpdate(
			ctx,
			tx,
			firstWalletID,
		)
		if err != nil {
			return err
		}

		secondWallet, err := s.repo.GetWalletForUpdate(
			ctx,
			tx,
			secondWalletID,
		)
		if err != nil {
			return err
		}

		// Identify which locked wallet is source/destination.
		var sourceWallet *domain.Wallet
		var destinationWallet *domain.Wallet

		if firstWallet.ID == req.FromWalletID {
			sourceWallet = firstWallet
			destinationWallet = secondWallet
		} else {
			sourceWallet = secondWallet
			destinationWallet = firstWallet
		}

		// 5. Check available balance.
		if sourceWallet.Balance < req.Amount {

			if err := s.repo.UpdateTransferStatus(
				ctx,
				tx,
				transfer.ID,
				domain.StatusFailed,
			); err != nil {
				return err
			}

			transfer.Status = domain.StatusFailed
			businessErr = ErrInsufficientFunds

			// Return nil so the FAILED transfer + idempotency record
			// are committed.
			return nil
		}

		// 6. Calculate new balances.
		sourceNewBalance := sourceWallet.Balance - req.Amount
		destinationNewBalance := destinationWallet.Balance + req.Amount

		// 7. Debit source wallet.
		if err := s.repo.UpdateWalletBalance(
			ctx,
			tx,
			sourceWallet.ID,
			sourceNewBalance,
		); err != nil {
			return err
		}

		// 8. Credit destination wallet.
		if err := s.repo.UpdateWalletBalance(
			ctx,
			tx,
			destinationWallet.ID,
			destinationNewBalance,
		); err != nil {
			return err
		}

		now := time.Now().UTC()

		// 9. Create DEBIT ledger entry.
		debitEntry := &domain.LedgerEntry{
			ID:         uuid.New(),
			TransferID: transfer.ID,
			WalletID:   sourceWallet.ID,
			EntryType:  domain.Debit,
			Amount:     req.Amount,
			CreatedAt:  now,
		}

		if err := s.repo.CreateLedgerEntry(
			ctx,
			tx,
			debitEntry,
		); err != nil {
			return err
		}

		// 10. Create CREDIT ledger entry.
		creditEntry := &domain.LedgerEntry{
			ID:         uuid.New(),
			TransferID: transfer.ID,
			WalletID:   destinationWallet.ID,
			EntryType:  domain.Credit,
			Amount:     req.Amount,
			CreatedAt:  now,
		}

		if err := s.repo.CreateLedgerEntry(
			ctx,
			tx,
			creditEntry,
		); err != nil {
			return err
		}

		// 11. Mark transfer as PROCESSED.
		if err := s.repo.UpdateTransferStatus(
			ctx,
			tx,
			transfer.ID,
			domain.StatusProcessed,
		); err != nil {
			return err
		}

		transfer.Status = domain.StatusProcessed

		return nil
	})

	if err != nil {
		// Another request may have claimed the same idempotency key
		// concurrently. Check again after our transaction rolled back.
		existingTransfer, lookupErr :=
			s.repo.GetTransferByIdempotencyKey(
				ctx,
				req.IdempotencyKey,
			)

		if lookupErr == nil && existingTransfer != nil {
			return existingTransfer, nil
		}

		return nil, fmt.Errorf("execute transfer: %w", err)
	}

	if businessErr != nil {
		return transfer, businessErr
	}

	return transfer, nil
}

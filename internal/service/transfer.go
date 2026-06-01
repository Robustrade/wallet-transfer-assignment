package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/example/wallet-transfer-assignment/internal/domain"
	"github.com/example/wallet-transfer-assignment/internal/repository"
)

const maxRetries = 20

func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "serialization") ||
		strings.Contains(msg, "locked") ||
		strings.Contains(msg, "busy")
}

func retryDelay(attempt int) time.Duration {
	return time.Duration(5+attempt*5) * time.Millisecond
}

type TransferService struct {
	db              *sql.DB
	walletRepo      *repository.WalletRepo
	transferRepo    *repository.TransferRepo
	idempotencyRepo *repository.IdempotencyRepo
}

func NewTransferService(
	db *sql.DB,
	walletRepo *repository.WalletRepo,
	transferRepo *repository.TransferRepo,
	idempotencyRepo *repository.IdempotencyRepo,
) *TransferService {
	return &TransferService{
		db:              db,
		walletRepo:      walletRepo,
		transferRepo:    transferRepo,
		idempotencyRepo: idempotencyRepo,
	}
}

type CreateTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type CreateTransferResponse struct {
	TransferID string               `json:"transferId"`
	State      domain.TransferState `json:"state"`
	Message    string               `json:"message,omitempty"`
}

func (s *TransferService) CreateTransfer(ctx context.Context, req CreateTransferRequest) (*CreateTransferResponse, error) {
	if req.Amount <= 0 {
		return nil, domain.ErrInvalidAmount
	}
	if req.FromWalletID == req.ToWalletID {
		return nil, domain.ErrSameWallet
	}
	if req.IdempotencyKey == "" {
		return nil, fmt.Errorf("idempotency key is required")
	}

	// Check for existing transfer by idempotency key (before transaction)
	existing, err := s.transferRepo.GetByIdempotencyKey(ctx, req.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return &CreateTransferResponse{
			TransferID: existing.ID,
			State:      existing.State,
			Message:    "duplicate request",
		}, nil
	}

	for attempt := 0; attempt < maxRetries; attempt++ {
		resp, err := s.createTransferTx(ctx, req)
		if err == nil {
			return resp, nil
		}
		if !isRetryableError(err) {
			return nil, err
		}
		if attempt < maxRetries-1 {
			time.Sleep(retryDelay(attempt))
		}
	}

	return nil, fmt.Errorf("max retries exceeded")
}

func (s *TransferService) createTransferTx(ctx context.Context, req CreateTransferRequest) (*CreateTransferResponse, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Lock and read wallets
	fromWallet, err := s.walletRepo.GetByID(ctx, tx, req.FromWalletID)
	if err != nil {
		return nil, err
	}
	if fromWallet.Balance < req.Amount {
		return nil, domain.ErrInsufficientFunds
	}

	toWallet, err := s.walletRepo.GetByID(ctx, tx, req.ToWalletID)
	if err != nil {
		return nil, err
	}

	transferID := uuid.New().String()
	now := time.Now()

	transfer := &domain.Transfer{
		ID:             transferID,
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   fromWallet.ID,
		ToWalletID:     toWallet.ID,
		Amount:         req.Amount,
		State:          domain.TransferStatePending,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := s.transferRepo.Create(ctx, tx, transfer); err != nil {
		// UNIQUE constraint violation on idempotency_key means a concurrent request won
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			existing, lookupErr := s.transferRepo.GetByIdempotencyKey(ctx, req.IdempotencyKey)
			if lookupErr != nil {
				return nil, lookupErr
			}
			if existing != nil {
				return &CreateTransferResponse{
					TransferID: existing.ID,
					State:      existing.State,
					Message:    "duplicate request",
				}, nil
			}
		}
		return nil, err
	}

	// Atomic balance update
	if err := s.walletRepo.UpdateBalance(ctx, tx, fromWallet.ID, -req.Amount); err != nil {
		return nil, err
	}
	if err := s.walletRepo.UpdateBalance(ctx, tx, toWallet.ID, req.Amount); err != nil {
		return nil, err
	}

	// Double-entry ledger
	entries := []*domain.LedgerEntry{
		{WalletID: fromWallet.ID, TransferID: transferID, EntryType: "DEBIT", Amount: req.Amount},
		{WalletID: toWallet.ID, TransferID: transferID, EntryType: "CREDIT", Amount: req.Amount},
	}
	if err := s.transferRepo.CreateLedgerEntries(ctx, tx, entries); err != nil {
		return nil, err
	}

	// Update state to PROCESSED
	if err := s.transferRepo.UpdateState(ctx, tx, transferID, domain.TransferStateProcessed); err != nil {
		return nil, err
	}

	// Record idempotency key
	if err := s.idempotencyRepo.RecordKey(ctx, tx, req.IdempotencyKey, transferID); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return &CreateTransferResponse{
		TransferID: transferID,
		State:      domain.TransferStateProcessed,
	}, nil
}

func (s *TransferService) GetWallet(ctx context.Context, walletID string) (*domain.Wallet, error) {
	return s.walletRepo.GetByID(ctx, nil, walletID)
}

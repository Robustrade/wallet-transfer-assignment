package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/example/wallet-transfer-assignment/internal/domain"
	"github.com/example/wallet-transfer-assignment/internal/repository"
)

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

	// Fast-path duplicate check outside transaction
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

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Re-check idempotency inside transaction for race safety
	existing, err = s.transferRepo.GetByIdempotencyKey(ctx, req.IdempotencyKey)
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
		return nil, err
	}

	if err := s.walletRepo.UpdateBalance(ctx, tx, fromWallet.ID, -req.Amount); err != nil {
		return nil, err
	}
	if err := s.walletRepo.UpdateBalance(ctx, tx, toWallet.ID, req.Amount); err != nil {
		return nil, err
	}

	entries := []*domain.LedgerEntry{
		{WalletID: fromWallet.ID, TransferID: transferID, EntryType: "DEBIT", Amount: req.Amount},
		{WalletID: toWallet.ID, TransferID: transferID, EntryType: "CREDIT", Amount: req.Amount},
	}
	if err := s.transferRepo.CreateLedgerEntries(ctx, tx, entries); err != nil {
		return nil, err
	}

	if err := s.transferRepo.UpdateState(ctx, tx, transferID, domain.TransferStateProcessed); err != nil {
		return nil, err
	}

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

package service

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"wallet-transfer-assignment/internal/model"
	"wallet-transfer-assignment/internal/repository"
)

var (
	ErrInvalidAmount     = errors.New("amount must be greater than zero")
	ErrWalletNotFound    = errors.New("wallet not found")
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrSameWallet        = errors.New("source and destination wallets must be different")
)

type TransferService struct {
	db   *sql.DB
	repo *repository.Repository
}

func NewTransferService(db *sql.DB, repo *repository.Repository) *TransferService {
	return &TransferService{
		db:   db,
		repo: repo,
	}
}

func (s *TransferService) Transfer(req model.TransferRequest) (*model.Transfer, error) {
	if req.Amount <= 0 {
		return nil, ErrInvalidAmount
	}

	if req.FromWalletID == req.ToWalletID {
		return nil, ErrSameWallet
	}

	tx, err := s.db.Begin()
	if err := s.repo.LockIdempotencyKey(tx, req.IdempotencyKey); err != nil {
		return nil, fmt.Errorf("lock idempotency key: %w", err)
	}
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Check whether this request was already processed.
	existing, err := s.repo.GetTransferByIdempotencyKey(tx, req.IdempotencyKey)
	if err == nil {
		return existing, nil
	}

	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("check idempotency: %w", err)
	}

	// Lock wallets in a consistent order to reduce deadlocks.
	firstID, secondID := req.FromWalletID, req.ToWalletID
	if firstID > secondID {
		firstID, secondID = secondID, firstID
	}

	firstWallet, err := s.repo.GetWalletForUpdate(tx, firstID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrWalletNotFound
		}
		return nil, fmt.Errorf("lock first wallet: %w", err)
	}

	secondWallet, err := s.repo.GetWalletForUpdate(tx, secondID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrWalletNotFound
		}
		return nil, fmt.Errorf("lock second wallet: %w", err)
	}

	var fromWallet, toWallet *model.Wallet

	if firstWallet.ID == req.FromWalletID {
		fromWallet = firstWallet
		toWallet = secondWallet
	} else {
		fromWallet = secondWallet
		toWallet = firstWallet
	}

	if fromWallet.Balance < req.Amount {
		return nil, ErrInsufficientFunds
	}

	transfer := &model.Transfer{
		ID:             uuid.NewString(),
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		Status:         model.Processed,
	}

	if err := s.repo.CreateTransfer(tx, transfer); err != nil {
		return nil, fmt.Errorf("create transfer: %w", err)
	}

	if err := s.repo.UpdateWalletBalance(
		tx,
		fromWallet.ID,
		fromWallet.Balance-req.Amount,
	); err != nil {
		return nil, fmt.Errorf("debit wallet: %w", err)
	}

	if err := s.repo.UpdateWalletBalance(
		tx,
		toWallet.ID,
		toWallet.Balance+req.Amount,
	); err != nil {
		return nil, fmt.Errorf("credit wallet: %w", err)
	}

	debit := &model.LedgerEntry{
		TransferID: transfer.ID,
		WalletID:   fromWallet.ID,
		Type:       model.Debit,
		Amount:     req.Amount,
	}

	credit := &model.LedgerEntry{
		TransferID: transfer.ID,
		WalletID:   toWallet.ID,
		Type:       model.Credit,
		Amount:     req.Amount,
	}

	if err := s.repo.CreateLedgerEntry(tx, debit); err != nil {
		return nil, fmt.Errorf("create debit entry: %w", err)
	}

	if err := s.repo.CreateLedgerEntry(tx, credit); err != nil {
		return nil, fmt.Errorf("create credit entry: %w", err)
	}

	if err := s.repo.CreateIdempotencyRecord(
		tx,
		req.IdempotencyKey,
		transfer.ID,
		transfer.Status,
	); err != nil {
		return nil, fmt.Errorf("create idempotency record: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return transfer, nil
}

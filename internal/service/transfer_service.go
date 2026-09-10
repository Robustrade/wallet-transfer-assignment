package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
)

var (
	ErrInvalidTransferID = errors.New("transfer id cannot be empty")
)

const (
	defaultTransferLimit = 20
	maxTransferLimit     = 100
)

type TransferService struct {
	txManager    repository.Transactor
	walletRepo   repository.WalletRepository
	transferRepo repository.TransferRepository
	ledgerRepo   repository.LedgerRepository
	idempRepo    repository.IdempotencyRepository
}

func NewTransferService(
	txManager repository.Transactor,
	walletRepo repository.WalletRepository,
	transferRepo repository.TransferRepository,
	ledgerRepo repository.LedgerRepository,
	idempRepo repository.IdempotencyRepository,
) *TransferService {
	return &TransferService{
		txManager:    txManager,
		walletRepo:   walletRepo,
		transferRepo: transferRepo,
		ledgerRepo:   ledgerRepo,
		idempRepo:    idempRepo,
	}
}

func (s *TransferService) ExecuteTransfer(ctx context.Context, req domain.CreateTransferRequest) (*domain.TransferResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// 1. Fast-path: check idempotency cache before opening transaction or locking rows
	if replay, err := s.checkIdempotency(ctx, req); err != nil || replay != nil {
		return replay, err
	}

	var createdTransfer *domain.Transfer

	// 2. Atomic transaction: lock wallets, verify balances, update balances, record transfer, ledger & idempotency
	err := s.txManager.WithinTransaction(ctx, func(txCtx context.Context) error {
		fromWallet, toWallet, err := s.lockWallets(txCtx, req.FromWalletID, req.ToWalletID)
		if err != nil {
			return err
		}

		if fromWallet.Balance < req.Amount {
			createdTransfer, err = s.recordFailedTransfer(txCtx, req, domain.ErrReasonInsufficientFunds)
			return err
		}

		createdTransfer, err = s.executeTransferTransaction(txCtx, req, fromWallet, toWallet)
		return err
	})

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			if replay, idempErr := s.checkIdempotency(ctx, req); idempErr != nil {
				return nil, idempErr
			} else if replay != nil {
				return replay, nil
			}
		}
		return nil, err
	}

	if createdTransfer.Status == domain.StatusFailed {
		return nil, domain.ErrInsufficientFunds
	}

	body, err := json.Marshal(createdTransfer)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal transfer: %w", err)
	}
	return &domain.TransferResult{
		Transfer:     createdTransfer,
		IsReplay:     false,
		ResponseCode: http.StatusCreated,
		ResponseBody: body,
	}, nil
}

// checkIdempotency checks if a request has already been executed, returning the cached replay or conflict error.
func (s *TransferService) checkIdempotency(ctx context.Context, req domain.CreateTransferRequest) (*domain.TransferResult, error) {
	record, err := s.idempRepo.GetIdempotencyRecord(ctx, req.IdempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("check idempotency: %w", err)
	}
	if record == nil {
		return nil, nil
	}
	if record.RequestHash != req.CanonicalHash() {
		return nil, domain.ErrIdempotencyConflict
	}

	var transferPtr *domain.Transfer
	if record.ResponseCode < 400 {
		var cachedTransfer domain.Transfer
		if err := json.Unmarshal([]byte(record.ResponseBody), &cachedTransfer); err != nil {
			return nil, fmt.Errorf("failed to unmarshal cached transfer: %w", err)
		}
		transferPtr = &cachedTransfer
	}

	return &domain.TransferResult{
		Transfer:     transferPtr,
		IsReplay:     true,
		ResponseCode: record.ResponseCode,
		ResponseBody: []byte(record.ResponseBody),
	}, nil
}

// lockWallets locks both wallets in deterministic order and maps them to sender and receiver.
func (s *TransferService) lockWallets(ctx context.Context, fromID, toID string) (*domain.Wallet, *domain.Wallet, error) {
	wallets, err := s.walletRepo.GetWalletsForUpdate(ctx, fromID, toID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to lock wallets: %w", err)
	}
	if len(wallets) < 2 {
		return nil, nil, domain.ErrWalletNotFound
	}

	var fromWallet, toWallet *domain.Wallet
	for i := range wallets {
		switch wallets[i].ID {
		case fromID:
			fromWallet = &wallets[i]
		case toID:
			toWallet = &wallets[i]
		}
	}
	if fromWallet == nil || toWallet == nil {
		return nil, nil, domain.ErrWalletNotFound
	}
	return fromWallet, toWallet, nil
}

// recordFailedTransfer persists a FAILED transfer record and idempotency record for auditability without mutating balances or ledger.
func (s *TransferService) recordFailedTransfer(ctx context.Context, req domain.CreateTransferRequest, reason string) (*domain.Transfer, error) {
	failedTransfer := domain.Transfer{
		ID:             uuid.New().String(),
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		Status:         domain.StatusFailed,
		ErrorReason:    &reason,
	}
	tr, err := s.transferRepo.CreateTransfer(ctx, failedTransfer)
	if err != nil {
		return nil, fmt.Errorf("failed to record failed transfer: %w", err)
	}

	errResp := map[string]string{
		"error":   reason,
		"message": "insufficient wallet balance for transfer",
	}
	body, err := json.Marshal(errResp)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal error response for idempotency: %w", err)
	}

	idempRecord := domain.IdempotencyRecord{
		Key:          req.IdempotencyKey,
		RequestHash:  req.CanonicalHash(),
		ResponseCode: http.StatusUnprocessableEntity,
		ResponseBody: string(body),
	}
	if _, err := s.idempRepo.CreateIdempotencyRecord(ctx, idempRecord); err != nil {
		return nil, fmt.Errorf("failed to save idempotency record for failed transfer: %w", err)
	}

	return tr, nil
}

// executeTransferTransaction orchestrates balance debits/credits, transfer creation, double-entry ledger, and idempotency persistence.
func (s *TransferService) executeTransferTransaction(
	ctx context.Context,
	req domain.CreateTransferRequest,
	fromWallet, toWallet *domain.Wallet,
) (*domain.Transfer, error) {
	if err := s.walletRepo.UpdateWalletBalance(ctx, fromWallet.ID, fromWallet.Balance-req.Amount); err != nil {
		return nil, fmt.Errorf("failed to update sender balance: %w", err)
	}
	if err := s.walletRepo.UpdateWalletBalance(ctx, toWallet.ID, toWallet.Balance+req.Amount); err != nil {
		return nil, fmt.Errorf("failed to update receiver balance: %w", err)
	}

	transfer := domain.Transfer{
		ID:             uuid.New().String(),
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		Status:         domain.StatusProcessed,
	}
	createdTransfer, err := s.transferRepo.CreateTransfer(ctx, transfer)
	if err != nil {
		return nil, fmt.Errorf("failed to create transfer: %w", err)
	}

	entries := []domain.LedgerEntry{
		{ID: uuid.New().String(), TransferID: createdTransfer.ID, WalletID: req.FromWalletID, Type: domain.LedgerEntryDebit, Amount: req.Amount},
		{ID: uuid.New().String(), TransferID: createdTransfer.ID, WalletID: req.ToWalletID, Type: domain.LedgerEntryCredit, Amount: req.Amount},
	}
	for _, entry := range entries {
		if _, err := s.ledgerRepo.CreateLedgerEntry(ctx, entry); err != nil {
			return nil, fmt.Errorf("failed to create %s ledger entry: %w", entry.Type, err)
		}
	}

	body, err := json.Marshal(createdTransfer)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal transfer for idempotency: %w", err)
	}

	idempRecord := domain.IdempotencyRecord{
		Key:          req.IdempotencyKey,
		RequestHash:  req.CanonicalHash(),
		ResponseCode: http.StatusOK,
		ResponseBody: string(body),
	}
	if _, err := s.idempRepo.CreateIdempotencyRecord(ctx, idempRecord); err != nil {
		return nil, fmt.Errorf("failed to save idempotency record: %w", err)
	}

	return createdTransfer, nil
}

// GetTransfer retrieves a transfer by its ID.
func (s *TransferService) GetTransfer(ctx context.Context, id string) (*domain.Transfer, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrInvalidTransferID
	}
	transfer, err := s.transferRepo.GetTransfer(ctx, id)
	if err != nil {
		return nil, err
	}
	if transfer == nil {
		return nil, domain.ErrTransferNotFound
	}
	return transfer, nil
}

// ListWalletTransfers retrieves transfers involving the specified wallet with pagination.
func (s *TransferService) ListWalletTransfers(ctx context.Context, walletID string, limit, offset int32) ([]domain.Transfer, error) {
	walletID = strings.TrimSpace(walletID)
	if walletID == "" {
		return nil, ErrInvalidWalletID
	}

	if _, err := s.walletRepo.GetWallet(ctx, walletID); err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = defaultTransferLimit
	} else if limit > maxTransferLimit {
		limit = maxTransferLimit
	}

	if offset < 0 {
		offset = 0
	}

	return s.transferRepo.ListTransfersByWalletID(ctx, walletID, limit, offset)
}

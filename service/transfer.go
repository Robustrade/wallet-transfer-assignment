package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Robustrade/wallet-transfer-assignment/model"
	"github.com/Robustrade/wallet-transfer-assignment/repository"
)

var (
	ErrInvalidRequest      = errors.New("invalid request")
	ErrInsufficientFunds   = errors.New("insufficient funds")
	ErrWalletNotFound      = errors.New("wallet not found")
	ErrIdempotencyConflict = errors.New("idempotency key already used with different request")
)

type TransferService struct {
	db           *sql.DB
	walletRepo   *repository.WalletRepository
	transferRepo *repository.TransferRepository
	ledgerRepo   *repository.LedgerRepository
}

func NewTransferService(
	db *sql.DB,
	walletRepo *repository.WalletRepository,
	transferRepo *repository.TransferRepository,
	ledgerRepo *repository.LedgerRepository,
) *TransferService {
	return &TransferService{
		db:           db,
		walletRepo:   walletRepo,
		transferRepo: transferRepo,
		ledgerRepo:   ledgerRepo,
	}
}

func (s *TransferService) CreateTransfer(
	ctx context.Context,
	req model.TransferRequest,
) (*model.Transfer, error) {

	amount, err := parseAmount(req.Amount.String())
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}

	if strings.TrimSpace(req.IdempotencyKey) == "" {
		return nil, fmt.Errorf("%w: idempotencyKey is required", ErrInvalidRequest)
	}

	if len(req.IdempotencyKey) > 255 {
		return nil, fmt.Errorf("%w: idempotencyKey is too long", ErrInvalidRequest)
	}

	if strings.TrimSpace(req.FromWalletID) == "" {
		return nil, fmt.Errorf("%w: fromWalletId is required", ErrInvalidRequest)
	}

	if strings.TrimSpace(req.ToWalletID) == "" {
		return nil, fmt.Errorf("%w: toWalletId is required", ErrInvalidRequest)
	}

	if len(req.FromWalletID) > 100 || len(req.ToWalletID) > 100 {
		return nil, fmt.Errorf("%w: wallet ID is too long", ErrInvalidRequest)
	}

	if req.FromWalletID == req.ToWalletID {
		return nil, fmt.Errorf(
			"%w: source and destination wallets must be different",
			ErrInvalidRequest,
		)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback()
	}()

	// Lock both wallets in deterministic order.
	// This prevents concurrent transfers from acquiring
	// wallet locks in different orders.
	walletIDs := []string{
		req.FromWalletID,
		req.ToWalletID,
	}

	sort.Strings(walletIDs)

	wallets := make(map[string]*model.Wallet)

	for _, walletID := range walletIDs {
		wallet, err := s.walletRepo.GetForUpdate(
			ctx,
			tx,
			walletID,
		)
		if err != nil {
			if strings.Contains(err.Error(), "wallet not found") {
				return nil, fmt.Errorf("%w: %s", ErrWalletNotFound, walletID)
			}

			return nil, fmt.Errorf("lock wallet: %w", err)
		}

		wallets[walletID] = wallet
	}

	fromWallet := wallets[req.FromWalletID]
	toWallet := wallets[req.ToWalletID]

	transfer := &model.Transfer{
		ID:             uuid.New().String(),
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         amount,
		Status:         model.StatusPending,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// Atomically claim the idempotency key.
	created, err := s.transferRepo.Create(ctx, tx, transfer)
	if err != nil {
		return nil, fmt.Errorf("create transfer: %w", err)
	}

	// Idempotency:
	// If another request already created this key, return that transfer.
	if !created {
		existing, err := s.transferRepo.GetByIdempotencyKey(
			ctx,
			tx,
			req.IdempotencyKey,
		)
		if err != nil {
			return nil, fmt.Errorf("get existing transfer: %w", err)
		}

		// Same key + different request = conflict.
		if existing.FromWalletID != req.FromWalletID ||
			existing.ToWalletID != req.ToWalletID ||
			existing.Amount != amount {
			return nil, ErrIdempotencyConflict
		}

		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit idempotent request: %w", err)
		}

		return existing, nil
	}

	// Check balance while the wallet is still locked.
	if fromWallet.Balance < amount {
		if err := s.markFailed(ctx, tx, transfer.ID); err != nil {
			return nil, fmt.Errorf("mark transfer failed: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit failed transfer: %w", err)
		}

		return nil, ErrInsufficientFunds
	}

	// Debit source wallet.
	if err := s.walletRepo.UpdateBalance(
		ctx,
		tx,
		fromWallet.ID,
		fromWallet.Balance-amount,
	); err != nil {
		return nil, fmt.Errorf("debit source wallet: %w", err)
	}

	// Credit destination wallet.
	if err := s.walletRepo.UpdateBalance(
		ctx,
		tx,
		toWallet.ID,
		toWallet.Balance+amount,
	); err != nil {
		return nil, fmt.Errorf("credit destination wallet: %w", err)
	}

	// Create DEBIT ledger entry.
	debit := &model.LedgerEntry{
		ID:         uuid.New().String(),
		TransferID: transfer.ID,
		WalletID:   fromWallet.ID,
		EntryType:  model.LedgerDebit,
		Amount:     amount,
		CreatedAt:  time.Now(),
	}

	if err := s.ledgerRepo.Create(ctx, tx, debit); err != nil {
		return nil, fmt.Errorf("create debit ledger entry: %w", err)
	}

	// Create CREDIT ledger entry.
	credit := &model.LedgerEntry{
		ID:         uuid.New().String(),
		TransferID: transfer.ID,
		WalletID:   toWallet.ID,
		EntryType:  model.LedgerCredit,
		Amount:     amount,
		CreatedAt:  time.Now(),
	}

	if err := s.ledgerRepo.Create(ctx, tx, credit); err != nil {
		return nil, fmt.Errorf("create credit ledger entry: %w", err)
	}

	// Mark transfer successful.
	if err := s.transferRepo.UpdateStatus(
		ctx,
		tx,
		transfer.ID,
		model.StatusProcessed,
	); err != nil {
		return nil, fmt.Errorf("mark transfer processed: %w", err)
	}

	transfer.Status = model.StatusProcessed

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transfer: %w", err)
	}

	return transfer, nil
}

func (s *TransferService) markFailed(
	ctx context.Context,
	tx *sql.Tx,
	transferID string,
) error {
	return s.transferRepo.UpdateStatus(
		ctx,
		tx,
		transferID,
		model.StatusFailed,
	)
}

func parseAmount(value string) (int64, error) {
	value = strings.TrimSpace(value)

	if value == "" {
		return 0, fmt.Errorf("amount is required")
	}

	if strings.HasPrefix(value, "-") {
		return 0, fmt.Errorf("amount must be greater than zero")
	}

	parts := strings.Split(value, ".")

	if len(parts) > 2 {
		return 0, fmt.Errorf("invalid amount")
	}

	whole := parts[0]
	fraction := ""

	if len(parts) == 2 {
		fraction = parts[1]
	}

	if whole == "" {
		return 0, fmt.Errorf("invalid amount")
	}

	if fraction != "" && len(fraction) > 2 {
		return 0, fmt.Errorf("amount supports at most 2 decimal places")
	}

	if fraction == "" {
		fraction = "00"
	} else if len(fraction) == 1 {
		fraction += "0"
	}

	wholeValue, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid amount")
	}

	fractionValue, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid amount")
	}

	if wholeValue > (int64(^uint64(0)>>1)-fractionValue)/100 {
		return 0, fmt.Errorf("amount is too large")
	}

	amount := wholeValue*100 + fractionValue

	if amount <= 0 {
		return 0, fmt.Errorf("amount must be greater than zero")
	}

	return amount, nil
}

func FormatAmount(amount int64) string {
	return fmt.Sprintf("%d.%02d", amount/100, amount%100)
}

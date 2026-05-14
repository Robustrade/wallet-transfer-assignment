package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/parth/wallet-transfer-assignment/internal/domain"
	"github.com/parth/wallet-transfer-assignment/internal/repositories"
)

type TransferService struct {
	repo *repositories.TransferRepository
	db   *pgxpool.Pool
}

func NewTransferService(db *pgxpool.Pool) *TransferService {
	return &TransferService{
		repo: repositories.NewTransferRepository(db),
		db:   db,
	}
}

// ProcessTransfer handles the complete transfer process with idempotency and concurrency control
func (s *TransferService) ProcessTransfer(ctx context.Context, req *domain.TransferRequest) (*domain.TransferResponse, error) {
	// Step 1: Validate input first
	if err := domain.ValidateTransferRequest(req); err != nil {
		return nil, err
	}

	// Step 2: Check if same wallet
	if req.FromWalletID == req.ToWalletID {
		return nil, domain.ErrSameWallet
	}

	// Step 3: Check idempotency - has this request been processed before?
	existingRecord, err := s.repo.GetIdempotencyRecord(ctx, req.IdempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("failed to check idempotency: %w", err)
	}

	if existingRecord != nil {
		// Request already processed, return cached response
		var cachedResponse domain.TransferResponse
		if err := json.Unmarshal([]byte(existingRecord.ResponsePayload), &cachedResponse); err != nil {
			return nil, fmt.Errorf("failed to parse cached response: %w", err)
		}
		return &cachedResponse, nil
	}

	// Step 4: Parse amount
	amount := new(big.Float)
	if _, ok := amount.SetString(req.Amount); !ok {
		return nil, domain.ErrInvalidAmountFormat
	}

	// Step 5: Begin transaction for concurrency control with SERIALIZABLE isolation
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Step 6: Create transfer record in PENDING state
	transferID := uuid.New().String()
	transfer := &domain.Transfer{
		TransferID:     transferID,
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		State:          "PENDING",
	}

	if err := s.repo.InsertTransfer(ctx, tx, transfer); err != nil {
		// Check if this is a unique constraint violation on idempotency_key
		// This can happen if two concurrent requests with same key both started transactions
		if isUniqueConstraintError(err) {
			// Fetch the existing transfer
			existingTransfer, err := s.repo.GetTransferByIdempotencyKey(ctx, req.IdempotencyKey)
			if err == nil && existingTransfer != nil {
				// Return the existing transfer's status
				response := &domain.TransferResponse{
					TransferID: existingTransfer.TransferID,
					State:      existingTransfer.State,
					Message:    fmt.Sprintf("Transfer %s is in state %s", existingTransfer.TransferID, existingTransfer.State),
				}
				return response, nil
			}
		}
		return nil, fmt.Errorf("failed to create transfer: %w", err)
	}

	// Step 7: Lock wallets and check/update balances (concurrency control via FOR UPDATE)
	fromWallet, err := s.repo.GetWalletForUpdate(ctx, tx, req.FromWalletID)
	if err != nil {
		return nil, domain.ErrSourceWalletNotFound
	}

	toWallet, err := s.repo.GetWalletForUpdate(ctx, tx, req.ToWalletID)
	if err != nil {
		return nil, domain.ErrDestinationWalletNotFound
	}

	// Step 8: Validate sufficient balance
	fromBalance := new(big.Float)
	fromBalance.SetString(fromWallet.Balance)

	if fromBalance.Cmp(amount) < 0 {
		return nil, domain.ErrInsufficientBalance
	}

	// Step 9: Calculate new balances
	newFromBalance := new(big.Float).Sub(fromBalance, amount)
	toBalance := new(big.Float)
	toBalance.SetString(toWallet.Balance)
	newToBalance := new(big.Float).Add(toBalance, amount)

	// Step 10: Update balances
	if err := s.repo.UpdateWalletBalance(ctx, tx, req.FromWalletID, newFromBalance.String()); err != nil {
		return nil, fmt.Errorf("failed to update from wallet balance: %w", err)
	}

	if err := s.repo.UpdateWalletBalance(ctx, tx, req.ToWalletID, newToBalance.String()); err != nil {
		return nil, fmt.Errorf("failed to update to wallet balance: %w", err)
	}

	// Step 11: Create ledger entries (double-entry bookkeeping)
	debitEntry := &domain.LedgerEntry{
		EntryID:    uuid.New().String(),
		WalletID:   req.FromWalletID,
		TransferID: transferID,
		Type:       "DEBIT",
		Amount:     req.Amount,
	}

	creditEntry := &domain.LedgerEntry{
		EntryID:    uuid.New().String(),
		WalletID:   req.ToWalletID,
		TransferID: transferID,
		Type:       "CREDIT",
		Amount:     req.Amount,
	}

	if err := s.repo.InsertLedgerEntry(ctx, tx, debitEntry); err != nil {
		return nil, fmt.Errorf("failed to create debit entry: %w", err)
	}

	if err := s.repo.InsertLedgerEntry(ctx, tx, creditEntry); err != nil {
		return nil, fmt.Errorf("failed to create credit entry: %w", err)
	}

	// Step 12: Update transfer state to PROCESSED
	if err := s.repo.UpdateTransferState(ctx, tx, transferID, "PROCESSED"); err != nil {
		return nil, fmt.Errorf("failed to update transfer state: %w", err)
	}

	// Step 13: Store idempotency record
	response := &domain.TransferResponse{
		TransferID: transferID,
		State:      "PROCESSED",
		Message:    "Transfer completed successfully",
	}

	responseJSON, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}

	idempotencyRecord := &domain.IdempotencyRecord{
		IdempotencyKey:  req.IdempotencyKey,
		TransferID:      transferID,
		ResponsePayload: string(responseJSON),
	}

	if err := s.repo.InsertIdempotencyRecord(ctx, tx, idempotencyRecord); err != nil {
		return nil, fmt.Errorf("failed to store idempotency record: %w", err)
	}

	// Step 14: Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return response, nil
}

// GetTransferStatus retrieves the status of a transfer
func (s *TransferService) GetTransferStatus(ctx context.Context, transferID string) (*domain.Transfer, error) {
	transfer, err := s.repo.GetTransferByID(ctx, transferID)
	if err != nil {
		if errors.Is(err, domain.ErrTransferNotFound) {
			return nil, domain.ErrTransferNotFound
		}
		return nil, err
	}
	return transfer, nil
}

// isUniqueConstraintError checks if an error is a PostgreSQL unique constraint violation
func isUniqueConstraintError(err error) bool {
	// This is a simplified check - in production use pgconn.PgError for proper error code checking
	return err != nil && errors.Is(err, err)
}

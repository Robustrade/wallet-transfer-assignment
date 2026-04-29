package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"
	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/repository"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TransferRequest represents a request to create a transfer
type TransferRequest struct {
	IdempotencyKey uuid.UUID `json:"idempotencyKey" binding:"required"`
	FromWalletID   uuid.UUID `json:"fromWalletId" binding:"required"`
	ToWalletID     uuid.UUID `json:"toWalletId" binding:"required"`
	Amount         int64     `json:"amount" binding:"required,gt=0"`
}

// TransferResponse represents the response of a transfer operation
type TransferResponse struct {
	TransferID   string    `json:"transferId"`
	State        string    `json:"state"`
	FromWalletID uuid.UUID `json:"fromWalletId"`
	ToWalletID   uuid.UUID `json:"toWalletId"`
	Amount       int64     `json:"amount"`
	CreatedAt    time.Time `json:"createdAt"`
}

// IdempotencyData stores both response and error information for replay
type IdempotencyData struct {
	Response     *TransferResponse `json:"response,omitempty"`
	Error        string            `json:"error,omitempty"`
	ErrorMessage string            `json:"errorMessage,omitempty"`
}

// TransferService handles transfer business logic
type TransferService struct {
	repo repository.Repository
}

func NewTransferService(repo repository.Repository) *TransferService {
	return &TransferService{repo: repo}
}

// CreateTransfer handles the complete transfer workflow with idempotency
func (s *TransferService) CreateTransfer(ctx context.Context, req TransferRequest) (*TransferResponse, error) {
	// Step 1: Fast-path check for existing idempotency record (outside transaction)
	// This is an optimization to avoid transaction overhead for duplicate requests
	existingRecord, err := s.repo.GetIdempotencyRecord(ctx, req.IdempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("failed to check idempotency: %w", err)
	}

	if existingRecord != nil {
		log.Printf("Idempotency key %s found, returning cached response", req.IdempotencyKey)
		var data IdempotencyData
		if err := json.Unmarshal(existingRecord.ResponseData, &data); err != nil {
			return nil, fmt.Errorf("failed to unmarshal cached response: %w", err)
		}
		// If original request resulted in error, return same error
		if data.Error != "" {
			return data.Response, mapErrorFromString(data.Error)
		}
		return data.Response, nil
	}

	// Step 2: Execute transfer in transaction with race-safe idempotency check
	response, err := s.executeTransfer(ctx, req)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// executeTransfer executes the transfer within a single transaction
func (s *TransferService) executeTransfer(ctx context.Context, req TransferRequest) (*TransferResponse, error) {
	// Begin transaction
	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Track whether transaction has been finalized (committed or rolled back)
	var txFinalized bool
	defer func() {
		if !txFinalized && tx != nil {
			// Transaction was neither committed nor explicitly rolled back, clean up
			_ = s.repo.RollbackTx(ctx, tx)
		}
	}()

	// Acquire exclusive lock on idempotency key to prevent concurrent duplicate requests
	// This blocks until any other transaction with the same key commits/rolls back
	if err := s.repo.AcquireIdempotencyLock(ctx, tx, req.IdempotencyKey); err != nil {
		return nil, fmt.Errorf("failed to acquire idempotency lock: %w", err)
	}

	// Race-safe idempotency check inside transaction
	existingRecord, err := s.repo.GetIdempotencyRecord(ctx, req.IdempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("failed to check idempotency in transaction: %w", err)
	}

	if existingRecord != nil {
		log.Printf("Idempotency key %s found during transaction, returning cached response", req.IdempotencyKey)
		// Explicitly rollback - no changes needed
		if err := s.repo.RollbackTx(ctx, tx); err != nil {
			log.Printf("Warning: failed to rollback transaction: %v", err)
		}
		txFinalized = true

		var data IdempotencyData
		if err := json.Unmarshal(existingRecord.ResponseData, &data); err != nil {
			return nil, fmt.Errorf("failed to unmarshal cached response: %w", err)
		}
		// If original request resulted in error, return same error
		if data.Error != "" {
			return data.Response, mapErrorFromString(data.Error)
		}
		return data.Response, nil
	}

	// Create transfer domain object
	transfer, err := domain.NewTransfer(req.FromWalletID, req.ToWalletID, req.Amount)
	if err != nil {
		return nil, err
	}

	// Create transfer record
	if err := s.repo.CreateTransfer(ctx, tx, transfer); err != nil {
		return nil, fmt.Errorf("failed to create transfer: %w", err)
	}

	// Lock wallets in consistent order to prevent deadlocks
	walletIDs := []uuid.UUID{req.FromWalletID, req.ToWalletID}
	wallets, err := s.repo.GetWalletsForUpdate(ctx, tx, walletIDs)
	if err != nil {
		// Mark transfer as failed if wallets not found
		if updateErr := s.repo.UpdateTransferState(ctx, tx, transfer.ID, domain.TransferStateFailed); updateErr != nil {
			// If we can't mark as failed, rollback and return original error
			return nil, err
		}

		// Build failed response
		failedResponse := &TransferResponse{
			TransferID:   transfer.ID.String(),
			State:        string(domain.TransferStateFailed),
			FromWalletID: transfer.FromWalletID,
			ToWalletID:   transfer.ToWalletID,
			Amount:       transfer.Amount,
			CreatedAt:    transfer.CreatedAt,
		}

		// Store idempotency record for failed transfer
		// This is critical - if we can't store idempotency, we must rollback
		if idempErr := s.storeIdempotencyRecordWithError(ctx, tx, req.IdempotencyKey, transfer.ID, failedResponse, err); idempErr != nil {
			// Failed to store idempotency, rollback everything
			log.Printf("Failed to store idempotency record for failed transfer: %v", idempErr)
			return nil, fmt.Errorf("failed to store idempotency record: %w", idempErr)
		}

		// Commit the failed transfer state with idempotency record
		if commitErr := s.repo.CommitTx(ctx, tx); commitErr != nil {
			return nil, fmt.Errorf("failed to commit failed transfer: %w", commitErr)
		}
		txFinalized = true
		return nil, err
	}

	// Find source and destination wallets
	var fromWallet, toWallet *domain.Wallet
	for _, w := range wallets {
		if w.ID == req.FromWalletID {
			fromWallet = w
		} else if w.ID == req.ToWalletID {
			toWallet = w
		}
	}

	// Check sufficient funds
	if !fromWallet.CanDebit(req.Amount) {
		log.Printf("Insufficient funds in wallet %s: balance=%d, required=%d",
			fromWallet.ID.String(), fromWallet.Balance, req.Amount)

		if err := transfer.MarkAsFailed(); err != nil {
			return nil, err
		}
		if err := s.repo.UpdateTransferState(ctx, tx, transfer.ID, domain.TransferStateFailed); err != nil {
			return nil, err
		}

		// Build failed response
		response := &TransferResponse{
			TransferID:   transfer.ID.String(),
			State:        string(transfer.State),
			FromWalletID: transfer.FromWalletID,
			ToWalletID:   transfer.ToWalletID,
			Amount:       transfer.Amount,
			CreatedAt:    transfer.CreatedAt,
		}

		// Store idempotency record for failed transfer
		if err := s.storeIdempotencyRecordWithError(ctx, tx, req.IdempotencyKey, transfer.ID, response, domain.ErrInsufficientFunds); err != nil {
			return nil, fmt.Errorf("failed to store idempotency record for failed transfer: %w", err)
		}

		// Commit transaction
		if err := s.repo.CommitTx(ctx, tx); err != nil {
			return nil, err
		}
		txFinalized = true

		return nil, domain.ErrInsufficientFunds
	}

	// Update balances
	newFromBalance := fromWallet.Balance - req.Amount
	newToBalance := toWallet.Balance + req.Amount

	if err := s.repo.UpdateWalletBalance(ctx, tx, fromWallet.ID, newFromBalance); err != nil {
		return nil, fmt.Errorf("failed to update from wallet: %w", err)
	}

	if err := s.repo.UpdateWalletBalance(ctx, tx, toWallet.ID, newToBalance); err != nil {
		return nil, fmt.Errorf("failed to update to wallet: %w", err)
	}

	// Create double-entry ledger entries
	debitEntry, err := domain.NewLedgerEntry(req.FromWalletID, transfer.ID, domain.EntryTypeDebit, req.Amount)
	if err != nil {
		return nil, err
	}

	creditEntry, err := domain.NewLedgerEntry(req.ToWalletID, transfer.ID, domain.EntryTypeCredit, req.Amount)
	if err != nil {
		return nil, err
	}

	if err := s.repo.CreateLedgerEntry(ctx, tx, debitEntry); err != nil {
		return nil, fmt.Errorf("failed to create debit entry: %w", err)
	}

	if err := s.repo.CreateLedgerEntry(ctx, tx, creditEntry); err != nil {
		return nil, fmt.Errorf("failed to create credit entry: %w", err)
	}

	// Mark transfer as processed
	if err := transfer.MarkAsProcessed(); err != nil {
		return nil, err
	}

	if err := s.repo.UpdateTransferState(ctx, tx, transfer.ID, domain.TransferStateProcessed); err != nil {
		return nil, fmt.Errorf("failed to update transfer state: %w", err)
	}

	// Build success response
	response := &TransferResponse{
		TransferID:   transfer.ID.String(),
		State:        string(transfer.State),
		FromWalletID: transfer.FromWalletID,
		ToWalletID:   transfer.ToWalletID,
		Amount:       transfer.Amount,
		CreatedAt:    transfer.CreatedAt,
	}

	// Store idempotency record before commit
	// The unique constraint on idempotency_key will prevent duplicate processing
	if err := s.storeIdempotencyRecord(ctx, tx, req.IdempotencyKey, transfer.ID, response); err != nil {
		// If this fails due to unique constraint violation, it means a concurrent
		// request already created the idempotency record, so we should return an error
		return nil, fmt.Errorf("failed to store idempotency record: %w", err)
	}

	// Commit transaction
	if err := s.repo.CommitTx(ctx, tx); err != nil {
		return nil, err
	}
	txFinalized = true

	log.Printf("Transfer %s completed successfully: %s -> %s, amount: %d",
		transfer.ID.String(), req.FromWalletID.String(), req.ToWalletID.String(), req.Amount)

	return response, nil
}

// storeIdempotencyRecord stores the idempotency record for successful transfers
func (s *TransferService) storeIdempotencyRecord(ctx context.Context, tx *gorm.DB, key uuid.UUID, transferID uuid.UUID, response *TransferResponse) error {
	data := IdempotencyData{
		Response: response,
	}

	responseData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal response: %w", err)
	}

	record := &domain.IdempotencyRecord{
		IdempotencyKey: key,
		TransferID:     transferID,
		ResponseData:   responseData,
		CreatedAt:      time.Now(),
	}

	if err := s.repo.CreateIdempotencyRecord(ctx, tx, record); err != nil {
		return fmt.Errorf("failed to store idempotency record: %w", err)
	}

	return nil
}

// storeIdempotencyRecordWithError stores the idempotency record for failed transfers
func (s *TransferService) storeIdempotencyRecordWithError(ctx context.Context, tx *gorm.DB, key uuid.UUID, transferID uuid.UUID, response *TransferResponse, domainErr error) error {
	data := IdempotencyData{
		Response:     response,
		Error:        errorToString(domainErr),
		ErrorMessage: domainErr.Error(),
	}

	responseData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal response: %w", err)
	}

	record := &domain.IdempotencyRecord{
		IdempotencyKey: key,
		TransferID:     transferID,
		ResponseData:   responseData,
		CreatedAt:      time.Now(),
	}

	if err := s.repo.CreateIdempotencyRecord(ctx, tx, record); err != nil {
		return fmt.Errorf("failed to store idempotency record: %w", err)
	}

	return nil
}

// errorToString converts domain errors to string identifiers
func errorToString(err error) string {
	switch err {
	case domain.ErrInsufficientFunds:
		return "INSUFFICIENT_FUNDS"
	case domain.ErrWalletNotFound:
		return "WALLET_NOT_FOUND"
	case domain.ErrInvalidAmount:
		return "INVALID_AMOUNT"
	case domain.ErrSameWallet:
		return "SAME_WALLET"
	case domain.ErrTransferNotFound:
		return "TRANSFER_NOT_FOUND"
	case domain.ErrInvalidState:
		return "INVALID_STATE"
	case domain.ErrInvalidStateTransition:
		return "INVALID_STATE_TRANSITION"
	default:
		return "INTERNAL_ERROR"
	}
}

// mapErrorFromString converts string identifiers back to domain errors
func mapErrorFromString(errStr string) error {
	switch errStr {
	case "INSUFFICIENT_FUNDS":
		return domain.ErrInsufficientFunds
	case "WALLET_NOT_FOUND":
		return domain.ErrWalletNotFound
	case "INVALID_AMOUNT":
		return domain.ErrInvalidAmount
	case "SAME_WALLET":
		return domain.ErrSameWallet
	case "TRANSFER_NOT_FOUND":
		return domain.ErrTransferNotFound
	case "INVALID_STATE":
		return domain.ErrInvalidState
	case "INVALID_STATE_TRANSITION":
		return domain.ErrInvalidStateTransition
	default:
		return fmt.Errorf("internal error")
	}
}

// GetTransfer retrieves a transfer by ID
func (s *TransferService) GetTransfer(ctx context.Context, transferID string) (*TransferResponse, error) {
	transferUUID, err := uuid.Parse(transferID)
	if err != nil {
		return nil, fmt.Errorf("invalid transfer ID: %w", err)
	}

	transfer, err := s.repo.GetTransferByID(ctx, transferUUID)
	if err != nil {
		return nil, err
	}

	return &TransferResponse{
		TransferID:   transfer.ID.String(),
		State:        string(transfer.State),
		FromWalletID: transfer.FromWalletID,
		ToWalletID:   transfer.ToWalletID,
		Amount:       transfer.Amount,
		CreatedAt:    transfer.CreatedAt,
	}, nil
}

package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"
	"wallet-transfer-service/internal/models"
	"wallet-transfer-service/internal/repositories"

	"github.com/google/uuid"
)

// TransferService handles transfer business logic
type TransferService struct {
	txManager       repositories.TransactionManager
	walletRepo      repositories.WalletRepository
	transferRepo    repositories.TransferRepository
	ledgerRepo      repositories.LedgerRepository
	idempotencyRepo repositories.IdempotencyRepository
}

// NewTransferService creates a new transfer service
func NewTransferService(
	txManager repositories.TransactionManager,
	walletRepo repositories.WalletRepository,
	transferRepo repositories.TransferRepository,
	ledgerRepo repositories.LedgerRepository,
	idempotencyRepo repositories.IdempotencyRepository,
) *TransferService {
	return &TransferService{
		txManager:       txManager,
		walletRepo:      walletRepo,
		transferRepo:    transferRepo,
		ledgerRepo:      ledgerRepo,
		idempotencyRepo: idempotencyRepo,
	}
}

// CreateTransfer creates a new transfer with idempotency and concurrency safety
func (s *TransferService) CreateTransfer(req *models.CreateTransferRequest) (*models.TransferResponse, error) {
	// Validate request
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Generate request hash for payload validation
	requestHash := models.GenerateRequestHash(req.FromWalletID, req.ToWalletID, req.Amount)

	// BEGIN TRANSACTION
	tx, err := s.txManager.BeginTx()
	if err != nil {
		return nil, fmt.Errorf("failed to start transaction: %w", err)
	}
	defer s.txManager.Rollback(tx) // Auto-rollback if not committed

	// STEP 1: Atomic idempotency claim
	idempotencyRecord := &models.IdempotencyRecord{
		IdempotencyKey: req.IdempotencyKey,
		RequestHash:    requestHash,
		Status:         models.IdempotencyStatusProcessing,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	err = s.idempotencyRepo.Create(tx, idempotencyRecord)
	if err != nil {
		// Duplicate key - first transaction will be rolled back by defer
		// Start new transaction to fetch existing record
		newTx, txErr := s.txManager.BeginTx()
		if txErr != nil {
			return nil, fmt.Errorf("failed to start transaction for duplicate check: %w", txErr)
		}
		defer s.txManager.Rollback(newTx)

		existingRecord, getErr := s.idempotencyRepo.GetByKey(newTx, req.IdempotencyKey)
		if getErr != nil {
			return nil, fmt.Errorf("failed to fetch existing idempotency record: %w", getErr)
		}
		if existingRecord == nil {
			return nil, fmt.Errorf("idempotency record not found after conflict: %w", err)
		}

		// Commit the read transaction
		if commitErr := s.txManager.Commit(newTx); commitErr != nil {
			return nil, fmt.Errorf("failed to commit read transaction: %w", commitErr)
		}

		// Validate request hash matches
		if validateErr := existingRecord.ValidateRequestHash(requestHash); validateErr != nil {
			return nil, &ConflictError{Message: validateErr.Error()}
		}

		// Handle based on status (original tx will be rolled back by defer)
		return s.handleDuplicateRequest(existingRecord)
	}

	// STEP 2: Lock wallets in deterministic order (prevents deadlocks)
	walletIDs := []string{req.FromWalletID, req.ToWalletID}
	sort.Strings(walletIDs) // Lexicographic order

	var fromWallet *models.Wallet
	for _, walletID := range walletIDs {
		wallet, err := s.walletRepo.GetByIDForUpdate(tx, walletID)
		if err != nil {
			// Return the actual error - don't mask DB errors or other issues
			return nil, fmt.Errorf("failed to lock wallet %s: %w", walletID, err)
		}

		if walletID == req.FromWalletID {
			fromWallet = wallet
		}
	}

	// STEP 3: Validate business rules
	if !fromWallet.HasSufficientBalance(req.Amount) {
		// Don't cache insufficient balance - balance might change on retry
		// Just rollback and return error (defer will handle rollback)
		return nil, &BusinessError{Message: "insufficient balance"}
	}

	// STEP 4: Create transfer record
	transferID := uuid.New().String()
	transfer := &models.Transfer{
		ID:           transferID,
		FromWalletID: req.FromWalletID,
		ToWalletID:   req.ToWalletID,
		Amount:       req.Amount,
		Status:       models.TransferStatusPending,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := s.transferRepo.Create(tx, transfer); err != nil {
		return nil, fmt.Errorf("failed to create transfer: %w", err)
	}

	// STEP 5: Create double-entry ledger entries
	debitEntry := models.NewDebitEntry(
		uuid.New().String(),
		req.FromWalletID,
		transferID,
		req.Amount,
	)
	if err := s.ledgerRepo.CreateEntry(tx, debitEntry); err != nil {
		return nil, fmt.Errorf("failed to create debit entry: %w", err)
	}

	creditEntry := models.NewCreditEntry(
		uuid.New().String(),
		req.ToWalletID,
		transferID,
		req.Amount,
	)
	if err := s.ledgerRepo.CreateEntry(tx, creditEntry); err != nil {
		return nil, fmt.Errorf("failed to create credit entry: %w", err)
	}

	// STEP 6: Update balances atomically
	if err := s.walletRepo.UpdateBalance(tx, req.FromWalletID, req.Amount, false); err != nil {
		return nil, fmt.Errorf("failed to debit from wallet: %w", err)
	}

	if err := s.walletRepo.UpdateBalance(tx, req.ToWalletID, req.Amount, true); err != nil {
		return nil, fmt.Errorf("failed to credit to wallet: %w", err)
	}

	// STEP 7: Update transfer status
	if err := s.transferRepo.UpdateStatus(tx, transferID, models.TransferStatusProcessed); err != nil {
		return nil, fmt.Errorf("failed to update transfer status: %w", err)
	}

	// STEP 8: Update idempotency record with response
	response := &models.TransferResponse{
		ID:           transferID,
		FromWalletID: req.FromWalletID,
		ToWalletID:   req.ToWalletID,
		Amount:       req.Amount,
		Status:       string(models.TransferStatusProcessed),
		CreatedAt:    transfer.CreatedAt.Format(time.RFC3339),
	}

	responseData, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}

	idempotencyRecord.MarkAsCompleted(transferID, responseData)
	if err := s.idempotencyRepo.Update(tx, idempotencyRecord); err != nil {
		return nil, fmt.Errorf("failed to update idempotency record: %w", err)
	}

	// COMMIT TRANSACTION
	if err := s.txManager.Commit(tx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return response, nil
}

// handleDuplicateRequest handles requests with duplicate idempotency keys
func (s *TransferService) handleDuplicateRequest(record *models.IdempotencyRecord) (*models.TransferResponse, error) {
	switch record.Status {
	case models.IdempotencyStatusProcessing:
		// Check if PROCESSING is stale (e.g., original request crashed)
		processingTimeout := 30 * time.Second // Configurable timeout
		if time.Since(record.CreatedAt) > processingTimeout {
			// PROCESSING is too old, likely the original request crashed
			// Return error so client can retry with a new idempotency key
			return nil, &ConflictError{Message: "original request timed out, please retry with a new idempotency key"}
		}
		// Request is still being processed (recent)
		return nil, &ConflictError{Message: "request is still processing, try again later"}

	case models.IdempotencyStatusCompleted:
		// Return cached response
		var response models.TransferResponse
		if err := json.Unmarshal(record.ResponseData, &response); err != nil {
			return nil, fmt.Errorf("failed to unmarshal cached response: %w", err)
		}
		return &response, nil

	case models.IdempotencyStatusFailed:
		// Return cached error
		return nil, &BusinessError{Message: string(record.ResponseData)}

	default:
		return nil, fmt.Errorf("unknown idempotency status: %s", record.Status)
	}
}

// markIdempotencyAsFailed updates idempotency record to FAILED status
func (s *TransferService) markIdempotencyAsFailed(tx *sql.Tx, key string, errorMessage string) {
	record, _ := s.idempotencyRepo.GetByKey(tx, key)
	if record != nil {
		record.MarkAsFailed([]byte(errorMessage))
		s.idempotencyRepo.Update(tx, record)
	}
}

// Custom error types
type ConflictError struct {
	Message string
}

func (e *ConflictError) Error() string {
	return e.Message
}

type BusinessError struct {
	Message string
}

func (e *BusinessError) Error() string {
	return e.Message
}

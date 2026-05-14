package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"

	"github.com/google/uuid"
	"github.com/manish8561/wallet-transfer-assignment/internal/models"
	"gorm.io/gorm"
)

// WalletRepository defines the interface for wallet persistence
type WalletRepository interface {
	GetForUpdate(ctx context.Context, tx *gorm.DB, walletID string) (*models.Wallet, error)
	UpdateBalance(ctx context.Context, tx *gorm.DB, walletID string, delta int64) error
	GetBalance(ctx context.Context, walletID string) (int64, error)
	Create(ctx context.Context, tx *gorm.DB, walletID string, initialBalance int64) (*models.Wallet, error)
}

// TransferRepository defines the interface for transfer persistence
type TransferRepository interface {
	Create(ctx context.Context, tx *gorm.DB, fromWalletID, toWalletID string, amount int64, transferID string) (*models.Transfer, error)
	UpdateStatus(ctx context.Context, tx *gorm.DB, transferID, newStatus string) error
	GetTransferHistory(ctx context.Context, walletID string, limit, offset int) ([]*models.Transfer, error)
}

// LedgerRepository defines the interface for ledger entry persistence
type LedgerRepository interface {
	Create(ctx context.Context, tx *gorm.DB, walletID, transferID, entryType string, amount int64, entryID string) (*models.LedgerEntry, error)
}

// IdempotencyRepository defines the interface for idempotency record persistence
type IdempotencyRepository interface {
	GetByKey(ctx context.Context, idempotencyKey string) (*models.IdempotencyRecord, error)
	Record(ctx context.Context, tx *gorm.DB, idempotencyKey, transferID string, response interface{}) error
}

// TransferService orchestrates wallet transfers
type TransferService struct {
	db              *gorm.DB
	walletRepo      WalletRepository
	transferRepo    TransferRepository
	ledgerRepo      LedgerRepository
	idempotencyRepo IdempotencyRepository
}

// NewTransferService creates a new transfer service
func NewTransferService(
	db *gorm.DB,
	walletRepo WalletRepository,
	transferRepo TransferRepository,
	ledgerRepo LedgerRepository,
	idempotencyRepo IdempotencyRepository,
) *TransferService {
	return &TransferService{
		db:              db,
		walletRepo:      walletRepo,
		transferRepo:    transferRepo,
		ledgerRepo:      ledgerRepo,
		idempotencyRepo: idempotencyRepo,
	}
}

// TransferResponse represents the API response for a transfer
type TransferResponse struct {
	TransferID   string `json:"transfer_id"`
	Status       string `json:"status"`
	FromWalletID string `json:"from_wallet_id"`
	ToWalletID   string `json:"to_wallet_id"`
	Amount       int64  `json:"amount"`
}

// ExecuteTransfer executes a wallet transfer with idempotency guarantees
func (s *TransferService) ExecuteTransfer(ctx context.Context, idempotencyKey, fromWalletID, toWalletID string, amount int64) (*TransferResponse, error) {
	// Validate input
	transfer := &models.Transfer{
		FromWalletID: fromWalletID,
		ToWalletID:   toWalletID,
		Amount:       amount,
	}
	if err := transfer.Validate(); err != nil {
		return nil, fmt.Errorf("validation error: %w", err)
	}

	// Check idempotency: if already processed, return cached result
	existing, err := s.idempotencyRepo.GetByKey(ctx, idempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("failed to check idempotency: %w", err)
	}
	if existing != nil {
		// Idempotent: return cached response
		var response TransferResponse
		if err := json.Unmarshal(existing.Response, &response); err != nil {
			return nil, fmt.Errorf("failed to unmarshal cached response: %w", err)
		}
		return &response, nil
	}

	// Begin transaction
	tx := s.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", tx.Error)
	}

	// Generate transfer ID
	transferID := uuid.New().String()

	// Create transfer record with PENDING status
	createdTransfer, err := s.transferRepo.Create(ctx, tx, fromWalletID, toWalletID, amount, transferID)
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create transfer: %w", err)
	}
	log.Printf("Created transfer: %v", createdTransfer)

	// Deterministic locking order to prevent deadlocks
	lockOrder := []string{fromWalletID, toWalletID}
	sort.Strings(lockOrder)

	for _, walletID := range lockOrder {
		_, err := s.walletRepo.GetForUpdate(ctx, tx, walletID)
		if err != nil {
			tx.Rollback()
			if err == gorm.ErrRecordNotFound {
				return nil, fmt.Errorf("wallet not found: %s", walletID)
			}
			return nil, fmt.Errorf("failed to lock wallet %s: %w", walletID, err)
		}
	}

	// Now that both are locked, retrieve fromWallet model for balance check within the transaction.
	// The GetForUpdate call above already locks the row, we just need to extract the fromWallet's data.
	var fromWalletModel *models.Wallet
	for _, walletID := range lockOrder {
		wallet, err := s.walletRepo.GetForUpdate(ctx, tx, walletID) // Re-fetch to get the actual object, or store it from the first loop
		if err != nil {
			tx.Rollback()
			if err == gorm.ErrRecordNotFound {
				return nil, fmt.Errorf("wallet not found: %s", walletID)
			}
			return nil, fmt.Errorf("failed to lock wallet %s: %w", walletID, err)
		}
		if walletID == fromWalletID {
			fromWalletModel = wallet
		}
	}

	// Verify sufficient balance
	if fromWalletModel.Balance < amount {
		tx.Rollback()
		return nil, fmt.Errorf("insufficient balance: have %d, need %d", fromWalletModel.Balance, amount)
	}

	// Create DEBIT ledger entry for from_wallet
	debitID := uuid.New().String()
	_, err = s.ledgerRepo.Create(ctx, tx, fromWalletID, transferID, models.EntryTypeDebit, amount, debitID)
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create debit ledger entry: %w", err)
	}

	// Create CREDIT ledger entry for to_wallet
	creditID := uuid.New().String()
	_, err = s.ledgerRepo.Create(ctx, tx, toWalletID, transferID, models.EntryTypeCredit, amount, creditID)
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create credit ledger entry: %w", err)
	}

	// Update from_wallet balance (debit)
	if err := s.walletRepo.UpdateBalance(ctx, tx, fromWalletID, -amount); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to update from_wallet balance: %w", err)
	}

	// Update to_wallet balance (credit)
	if err := s.walletRepo.UpdateBalance(ctx, tx, toWalletID, amount); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to update to_wallet balance: %w", err)
	}

	// Update transfer status to PROCESSED
	if err := s.transferRepo.UpdateStatus(ctx, tx, transferID, models.TransferStatusProcessed); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to update transfer status: %w", err)
	}

	// Build response
	response := &TransferResponse{
		TransferID:   transferID,
		Status:       models.TransferStatusProcessed,
		FromWalletID: fromWalletID,
		ToWalletID:   toWalletID,
		Amount:       amount,
	}

	// Record idempotency before commit
	if err := s.idempotencyRepo.Record(ctx, tx, idempotencyKey, transferID, response); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to record idempotency: %w", err)
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return response, nil
}

// GetBalance retrieves wallet balance
func (s *TransferService) GetBalance(ctx context.Context, walletID string) (int64, error) {
	return s.walletRepo.GetBalance(ctx, walletID)
}

// GetTransferHistory retrieves transfer history for a wallet
func (s *TransferService) GetTransferHistory(ctx context.Context, walletID string, limit, offset int) ([]*models.Transfer, error) {
	return s.transferRepo.GetTransferHistory(ctx, walletID, limit, offset)
}

// CreateWallet creates a new wallet with an initial balance
func (s *TransferService) CreateWallet(ctx context.Context, initialBalance int64) (*models.Wallet, error) {
	if initialBalance < 0 {
		return nil, fmt.Errorf("initial balance cannot be negative")
	}

	walletID := uuid.New().String()
	return s.walletRepo.Create(ctx, s.db, walletID, initialBalance)
}

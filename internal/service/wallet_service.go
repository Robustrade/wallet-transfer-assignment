package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dale-tomson/wallet-transfer-assignment/internal/domain"
	"github.com/dale-tomson/wallet-transfer-assignment/internal/repository"
	"github.com/google/uuid"
)

// TransferRequest represents the transfer payload matching ASSIGNMENT.md
type TransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"` // Minor units (e.g. cents)
}

// TransferResponse defines the standard payload returned to clients
type TransferResponse struct {
	TransferID string `json:"transferId"`
	Status     string `json:"status"`
	Amount     int64  `json:"amount"`
	Currency   string `json:"currency"`
	CreatedAt  string `json:"createdAt"`
}

type WalletService interface {
	CreateWallet(ctx context.Context, userID, currency string) (*domain.Wallet, error)
	GetWallet(ctx context.Context, walletID string) (*domain.Wallet, error)
	Transfer(ctx context.Context, req TransferRequest) (*TransferResponse, error)
}

type walletService struct {
	txManager       repository.TxManager
	walletRepo      repository.WalletRepository
	transferRepo    repository.TransferRepository
	idempotencyRepo repository.IdempotencyRepository
}

func NewWalletService(
	txManager repository.TxManager,
	walletRepo repository.WalletRepository,
	transferRepo repository.TransferRepository,
	idempotencyRepo repository.IdempotencyRepository,
) WalletService {
	return &walletService{
		txManager:       txManager,
		walletRepo:      walletRepo,
		transferRepo:    transferRepo,
		idempotencyRepo: idempotencyRepo,
	}
}

func (s *walletService) CreateWallet(ctx context.Context, userID, currency string) (*domain.Wallet, error) {
	if userID == "" || currency == "" {
		return nil, fmt.Errorf("user_id and currency are required")
	}

	wallet := &domain.Wallet{
		ID:        uuid.New().String(),
		UserID:    userID,
		Currency:  currency,
		Balance:   0,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	if err := s.walletRepo.Create(ctx, nil, wallet); err != nil {
		return nil, fmt.Errorf("failed to create wallet: %w", err)
	}

	return wallet, nil
}

func (s *walletService) GetWallet(ctx context.Context, walletID string) (*domain.Wallet, error) {
	if walletID == "" {
		return nil, domain.ErrWalletNotFound
	}
	return s.walletRepo.GetByID(ctx, nil, walletID)
}

func (s *walletService) Transfer(ctx context.Context, req TransferRequest) (*TransferResponse, error) {
	// 1. Basic Input Validation
	if req.IdempotencyKey == "" {
		return nil, domain.ErrIdempotencyKeyRequired
	}
	if req.FromWalletID == "" || req.ToWalletID == "" {
		return nil, fmt.Errorf("fromWalletId and toWalletId must be provided")
	}
	if req.FromWalletID == req.ToWalletID {
		return nil, domain.ErrSelfTransfer
	}
	if req.Amount <= 0 {
		return nil, domain.ErrInvalidAmount
	}

	// 2. Compute Payload SHA-256 Hash
	payloadBytes := fmt.Sprintf("%s|%s|%d", req.FromWalletID, req.ToWalletID, req.Amount)
	hashSum := sha256.Sum256([]byte(payloadBytes))
	requestHash := hex.EncodeToString(hashSum[:])

	// 3. Phase 1: Reserve Idempotency Lease (15-second TTL)
	lease, err := s.idempotencyRepo.Reserve(ctx, req.IdempotencyKey, requestHash, 15*time.Second)
	if err != nil {
		return nil, err
	}

	// Cached response replay check
	if lease.Status == domain.IdempotencyStatusResolved {
		if lease.RequestHash != requestHash {
			return nil, domain.ErrIdempotencyKeyConflict
		}
		var cachedResp TransferResponse
		if err := json.Unmarshal([]byte(lease.ResponseBody), &cachedResp); err != nil {
			return nil, fmt.Errorf("failed to unmarshal cached response: %w", err)
		}
		return &cachedResp, nil
	}

	// 4. Phase 2: Execute Atomic Transfer Transaction
	var finalResp *TransferResponse
	txErr := s.txManager.RunInTx(ctx, func(tx *sql.Tx) error {
		// Fetch source and destination wallets
		fromWallet, err := s.walletRepo.GetByID(ctx, tx, req.FromWalletID)
		if err != nil {
			return err
		}
		toWallet, err := s.walletRepo.GetByID(ctx, tx, req.ToWalletID)
		if err != nil {
			return err
		}

		// Currency consistency check
		if fromWallet.Currency != toWallet.Currency {
			return domain.ErrCurrencyMismatch
		}

		// Balance sufficiency check
		if fromWallet.Balance < req.Amount {
			return domain.ErrInsufficientFunds
		}

		now := time.Now().UTC()
		transferID := uuid.New().String()

		// Prepare immutable double-entry ledger records
		debitEntry := &domain.LedgerEntry{
			ID:         uuid.New().String(),
			TransferID: transferID,
			WalletID:   fromWallet.ID,
			Amount:     req.Amount,
			Direction:  domain.DirectionDebit,
			CreatedAt:  now,
		}

		creditEntry := &domain.LedgerEntry{
			ID:         uuid.New().String(),
			TransferID: transferID,
			WalletID:   toWallet.ID,
			Amount:     req.Amount,
			Direction:  domain.DirectionCredit,
			CreatedAt:  now,
		}

		transferRecord := &domain.Transfer{
			ID:             transferID,
			IdempotencyKey: req.IdempotencyKey,
			FromWalletID:   fromWallet.ID,
			ToWalletID:     toWallet.ID,
			Amount:         req.Amount,
			Currency:       fromWallet.Currency,
			Status:         domain.TransferStatusCompleted,
			CreatedAt:      now,
		}

		// Persist transfer and ledger entries
		if err := s.transferRepo.CreateTransferAndEntries(ctx, tx, transferRecord, debitEntry, creditEntry); err != nil {
			return fmt.Errorf("failed to save transfer and ledger entries: %w", err)
		}

		// Mutate wallet balances
		if err := s.walletRepo.UpdateBalances(ctx, tx, fromWallet.ID, toWallet.ID, req.Amount); err != nil {
			return fmt.Errorf("failed to update wallet balances: %w", err)
		}

		// Prepare final response
		finalResp = &TransferResponse{
			TransferID: transferID,
			Status:     string(domain.TransferStatusCompleted),
			Amount:     req.Amount,
			Currency:   fromWallet.Currency,
			CreatedAt:  now.Format(time.RFC3339),
		}

		respJSON, err := json.Marshal(finalResp)
		if err != nil {
			return fmt.Errorf("failed to serialize response body: %w", err)
		}

		// Commit status as RESOLVED inside the same transaction
		if err := s.idempotencyRepo.Resolve(ctx, tx, req.IdempotencyKey, 200, string(respJSON)); err != nil {
			return fmt.Errorf("failed to resolve idempotency lease: %w", err)
		}

		return nil
	})

	// 5. Cleanup lease on business/validation failure
	if txErr != nil {
		_ = s.idempotencyRepo.Release(ctx, req.IdempotencyKey)
		return nil, txErr
	}

	return finalResp, nil
}

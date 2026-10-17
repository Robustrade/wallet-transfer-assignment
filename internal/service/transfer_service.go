package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"github.com/google/uuid"
)

var errConcurrentTxRolledBack = errors.New("concurrent transaction rolled back")

// CreateTransferRequest represents the validated transfer request payload.
type CreateTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"` // in integer paise
}

// TransferResult wraps the outcome of a transfer operation.
type TransferResult struct {
	StatusCode   int    `json:"statusCode"`
	ResponseBody []byte `json:"responseBody"`
	Replayed     bool   `json:"replayed"`
}

// WalletBalanceResult represents the current balance of a wallet.
type WalletBalanceResult struct {
	WalletID  string    `json:"walletId"`
	UserID    string    `json:"userId"`
	Balance   int64     `json:"balance"` // in integer paise
	Currency  string    `json:"currency"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// TransferService defines business workflows for transfers and balances.
type TransferService interface {
	CreateTransfer(ctx context.Context, req *CreateTransferRequest) (*TransferResult, error)
	GetTransfer(ctx context.Context, id string) (*domain.Transfer, error)
	GetWalletBalance(ctx context.Context, walletID string) (*WalletBalanceResult, error)
}

type transferService struct {
	repo repository.WalletRepository
}

// NewTransferService constructs a TransferService instance.
func NewTransferService(repo repository.WalletRepository) TransferService {
	return &transferService{repo: repo}
}

// ComputeRequestHash generates a deterministic SHA-256 fingerprint from the normalized request fields:
// source wallet, destination wallet, and amount.
func ComputeRequestHash(fromWalletID, toWalletID string, amount int64) string {
	// JSON struct encoding gives the fields fixed names and boundaries. Delimiter-based
	// concatenation is ambiguous when identifiers themselves contain the delimiter.
	payload, err := json.Marshal(struct {
		FromWalletID string `json:"fromWalletId"`
		ToWalletID   string `json:"toWalletId"`
		Amount       int64  `json:"amount"`
	}{FromWalletID: fromWalletID, ToWalletID: toWalletID, Amount: amount})
	if err != nil {
		// The struct contains only strings and an integer, so encoding/json cannot fail.
		panic("unexpected error encoding transfer fingerprint: " + err.Error())
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// CreateTransfer coordinates the entire transactional transfer workflow with idempotency protection.
func (s *transferService) CreateTransfer(ctx context.Context, req *CreateTransferRequest) (*TransferResult, error) {
	// 1. Boundary & domain validation
	if err := s.validateRequest(req); err != nil {
		return nil, err
	}

	requestHash := ComputeRequestHash(req.FromWalletID, req.ToWalletID, req.Amount)

	// 2. Fast-path: Check if an idempotency record already exists
	existing, err := s.repo.GetIdempotencyRecord(ctx, req.IdempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("failed to check idempotency record: %w", err)
	}

	if existing != nil {
		// Key exists: check fingerprint
		if existing.RequestHash != requestHash {
			return nil, domain.ErrIdempotencyKeyReuseMismatch
		}

		// If already completed, replay the original saved response directly
		if existing.ResponseStatus > 0 {
			return &TransferResult{
				StatusCode:   existing.ResponseStatus,
				ResponseBody: []byte(existing.ResponseBody),
				Replayed:     true,
			}, nil
		}

		// Another request is in-flight: wait for its completion
		return s.waitForIdempotencyRecord(ctx, req.IdempotencyKey, requestHash)
	}

	// 3. Execute transactional transfer workflow (with up to 3 attempts in case of concurrent rollbacks)
	const maxAttempts = 4
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, err := s.executeTransferTransaction(ctx, req, requestHash)
		if err != nil {
			if errors.Is(err, errConcurrentTxRolledBack) {
				// A competing claim disappeared after rollback. Back off briefly to avoid
				// a tight retry loop under contention, while honoring caller cancellation.
				delay := time.Duration(attempt*attempt) * 25 * time.Millisecond
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil, ctx.Err()
				case <-timer.C:
				}
				continue
			}
			return nil, err
		}
		return result, nil
	}

	return nil, errors.New("could not claim idempotency key after concurrent transaction rollback; retry the request")
}

func (s *transferService) validateRequest(req *CreateTransferRequest) error {
	if req == nil {
		return domain.NewDomainError(domain.CodeInvalidRequest, "request body is required")
	}
	if req.IdempotencyKey == "" {
		return domain.ErrMissingIdempotencyKey
	}
	if len(req.IdempotencyKey) > 255 {
		return domain.NewDomainError(domain.CodeInvalidRequest, "idempotencyKey must be at most 255 bytes")
	}
	if req.FromWalletID == "" {
		return domain.ErrMissingFromWalletID
	}
	if len(req.FromWalletID) > 64 {
		return domain.NewDomainError(domain.CodeInvalidRequest, "fromWalletId must be at most 64 bytes")
	}
	if req.ToWalletID == "" {
		return domain.ErrMissingToWalletID
	}
	if len(req.ToWalletID) > 64 {
		return domain.NewDomainError(domain.CodeInvalidRequest, "toWalletId must be at most 64 bytes")
	}
	if req.FromWalletID == req.ToWalletID {
		return domain.ErrSameWalletTransfer
	}
	if err := domain.ValidateAmount(req.Amount); err != nil {
		return err
	}
	return nil
}

// executeTransferTransaction runs the atomic database transaction.
func (s *transferService) executeTransferTransaction(
	ctx context.Context,
	req *CreateTransferRequest,
	requestHash string,
) (*TransferResult, error) {
	var finalResult *TransferResult

	txErr := s.repo.WithTransaction(ctx, func(txRepo repository.WalletRepository) error {
		// a. Claim the idempotency record inside the transaction
		idemRecord := &domain.IdempotencyRecord{
			IdempotencyKey: req.IdempotencyKey,
			RequestHash:    requestHash,
			ResponseStatus: 0,
			ResponseBody:   "",
			CreatedAt:      time.Now().UTC(),
		}

		if err := txRepo.CreateIdempotencyRecord(ctx, idemRecord); err != nil {
			// If unique violation, another transaction claimed it first
			if repository.IsUniqueConstraintViolation(err) {
				return err // Will be caught outside to wait/poll for the concurrent result
			}
			return fmt.Errorf("failed to claim idempotency record: %w", err)
		}

		// b. Verify wallets exist and lock them in deterministic primary key order
		// Sorting wallet IDs (ORDER BY id ASC) eliminates deadlocks under opposing transfers (A->B and B->A)
		firstID, secondID := req.FromWalletID, req.ToWalletID
		if firstID > secondID {
			firstID, secondID = secondID, firstID
		}

		wallets, err := txRepo.GetWalletsForUpdate(ctx, firstID, secondID)
		if err != nil {
			return fmt.Errorf("failed to fetch wallets: %w", err)
		}
		if len(wallets) < 2 {
			// One or both wallets do not exist
			return domain.ErrWalletNotFound
		}

		// c. Create Transfer record in PENDING status
		transferID := "trn_" + uuid.New().String()
		now := time.Now().UTC()
		transfer := &domain.Transfer{
			ID:           transferID,
			FromWalletID: req.FromWalletID,
			ToWalletID:   req.ToWalletID,
			Amount:       req.Amount,
			Currency:     domain.CurrencyINR,
			Status:       domain.TransferStatusPending,
			CreatedAt:    now,
			UpdatedAt:    now,
		}

		if err := txRepo.CreateTransfer(ctx, transfer); err != nil {
			return fmt.Errorf("failed to create pending transfer: %w", err)
		}

		// d. Safe conditional debit:
		// UPDATE wallets SET balance = balance - $1 WHERE id = $2 AND balance >= $1
		rowsAffected, err := txRepo.DebitWalletConditional(ctx, req.FromWalletID, req.Amount)
		if err != nil {
			return fmt.Errorf("failed to execute conditional debit: %w", err)
		}

		if rowsAffected == 0 {
			// Insufficient funds: Business failure
			// Mark transfer FAILED, record 409 response in idempotency_records, and commit.
			failedStatus := domain.TransferStatusFailed
			failureCode := domain.FailureCodeInsufficientFunds
			transfer.Status = failedStatus
			transfer.FailureCode = &failureCode
			transfer.UpdatedAt = time.Now().UTC()
			if err := txRepo.UpdateTransfer(ctx, transfer); err != nil {
				return fmt.Errorf("failed to update transfer status to failed: %w", err)
			}

			errResp := map[string]any{
				"error": map[string]string{
					"code":    domain.CodeInsufficientFunds,
					"message": fmt.Sprintf("wallet %s has insufficient funds for transfer amount %d paise", req.FromWalletID, req.Amount),
				},
			}
			bodyBytes, _ := json.Marshal(errResp)

			completedAt := time.Now().UTC()
			idemRecord.ResponseStatus = http.StatusConflict
			idemRecord.ResponseBody = string(bodyBytes)
			idemRecord.TransferID = &transfer.ID
			idemRecord.CompletedAt = &completedAt
			if err := txRepo.UpdateIdempotencyRecord(ctx, idemRecord); err != nil {
				return fmt.Errorf("failed to update idempotency failure record: %w", err)
			}

			finalResult = &TransferResult{
				StatusCode:   http.StatusConflict,
				ResponseBody: bodyBytes,
				Replayed:     false,
			}
			return nil // Commit the failure transaction
		}

		// e. Debit succeeded: Credit destination wallet
		if err := txRepo.CreditWallet(ctx, req.ToWalletID, req.Amount); err != nil {
			return fmt.Errorf("failed to credit receiver wallet: %w", err)
		}

		// f. Double-Entry Ledger: Insert exactly two balanced records
		debitEntry := &domain.LedgerEntry{
			TransferID: transfer.ID,
			WalletID:   req.FromWalletID,
			EntryType:  domain.LedgerEntryDebit,
			Amount:     req.Amount,
			CreatedAt:  time.Now().UTC(),
		}
		creditEntry := &domain.LedgerEntry{
			TransferID: transfer.ID,
			WalletID:   req.ToWalletID,
			EntryType:  domain.LedgerEntryCredit,
			Amount:     req.Amount,
			CreatedAt:  time.Now().UTC(),
		}
		if err := txRepo.CreateLedgerEntries(ctx, debitEntry, creditEntry); err != nil {
			return fmt.Errorf("failed to insert double-entry ledger records: %w", err)
		}

		// g. Transition transfer state to PROCESSED
		transfer.Status = domain.TransferStatusProcessed
		transfer.UpdatedAt = time.Now().UTC()
		if err := txRepo.UpdateTransfer(ctx, transfer); err != nil {
			return fmt.Errorf("failed to update transfer status to processed: %w", err)
		}

		// h. Prepare 201 Created response and persist idempotency record
		successResp := map[string]any{
			"id":             transfer.ID,
			"idempotencyKey": req.IdempotencyKey,
			"fromWalletId":   transfer.FromWalletID,
			"toWalletId":     transfer.ToWalletID,
			"amount":         transfer.Amount,
			"currency":       transfer.Currency,
			"status":         transfer.Status,
			"createdAt":      transfer.CreatedAt.Format(time.RFC3339Nano),
		}
		bodyBytes, _ := json.Marshal(successResp)

		completedAt := time.Now().UTC()
		idemRecord.ResponseStatus = http.StatusCreated
		idemRecord.ResponseBody = string(bodyBytes)
		idemRecord.TransferID = &transfer.ID
		idemRecord.CompletedAt = &completedAt
		if err := txRepo.UpdateIdempotencyRecord(ctx, idemRecord); err != nil {
			return fmt.Errorf("failed to save idempotency success record: %w", err)
		}

		finalResult = &TransferResult{
			StatusCode:   http.StatusCreated,
			ResponseBody: bodyBytes,
			Replayed:     false,
		}
		return nil
	})

	if txErr != nil {
		if repository.IsUniqueConstraintViolation(txErr) {
			// A concurrent request won the claim. Wait for it to complete.
			return s.waitForIdempotencyRecord(ctx, req.IdempotencyKey, requestHash)
		}
		return nil, txErr
	}

	return finalResult, nil
}

// waitForIdempotencyRecord polls for the completion of a concurrent request using the same key.
func (s *transferService) waitForIdempotencyRecord(
	ctx context.Context,
	key, expectedHash string,
) (*TransferResult, error) {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.After(3 * time.Second)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout:
			return nil, errors.New("timed out waiting for concurrent request with identical idempotency key to complete")
		case <-ticker.C:
			record, err := s.repo.GetIdempotencyRecord(ctx, key)
			if err != nil {
				// Do not hide database failures by polling forever; the caller can retry
				// using the same idempotency key safely.
				return nil, fmt.Errorf("failed while waiting for idempotency record: %w", err)
			}
			if record == nil {
				// The concurrent transaction rolled back! Signal retry.
				return nil, errConcurrentTxRolledBack
			}
			if record.RequestHash != expectedHash {
				return nil, domain.ErrIdempotencyKeyReuseMismatch
			}
			if record.ResponseStatus > 0 {
				return &TransferResult{
					StatusCode:   record.ResponseStatus,
					ResponseBody: []byte(record.ResponseBody),
					Replayed:     true,
				}, nil
			}
		}
	}
}

func (s *transferService) GetTransfer(ctx context.Context, id string) (*domain.Transfer, error) {
	return s.repo.GetTransfer(ctx, id)
}

func (s *transferService) GetWalletBalance(ctx context.Context, walletID string) (*WalletBalanceResult, error) {
	wallet, err := s.repo.GetWallet(ctx, walletID)
	if err != nil {
		return nil, err
	}
	return &WalletBalanceResult{
		WalletID:  wallet.ID,
		UserID:    wallet.UserID,
		Balance:   wallet.Balance,
		Currency:  wallet.Currency,
		UpdatedAt: wallet.UpdatedAt,
	}, nil
}

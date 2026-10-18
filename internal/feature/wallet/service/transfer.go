package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"wallet-transfer-assignment/internal/feature/wallet/domain"
	"wallet-transfer-assignment/internal/feature/wallet/repository"

	"gorm.io/gorm"
)

var (
	ErrInvalidTransferRequest = errors.New("invalid transfer request")

	// ErrRequestInProgress means the idempotency key is claimed but its original
	// request has not stored a response yet; the client should retry later.
	ErrRequestInProgress = errors.New("request with this idempotency key is still in progress")
)

// maxAttempts bounds how often a transfer transaction is retried after a
// deadlock or serialization failure.
const maxAttempts = 3

type CreateTransferRequest struct {
	IdempotencyKey string
	FromWalletID   int64
	ToWalletID     int64
	Amount         int64
}

type TransferResult struct {
	StatusCode int
	Body       []byte
}

type TransferService interface {
	CreateTransfer(
		ctx context.Context,
		req CreateTransferRequest,
	) (*TransferResult, error)
}

type transferService struct {
	walletRepo      repository.WalletRepository
	transferRepo    repository.TransferRepository
	ledgerRepo      repository.LedgerRepository
	idempotencyRepo repository.IdempotencyRepository
	txManager       repository.TransactionManager
}

// Constructor
func NewTransferService(
	walletRepo repository.WalletRepository,
	transferRepo repository.TransferRepository,
	ledgerRepo repository.LedgerRepository,
	idempotencyRepo repository.IdempotencyRepository,
	txManager repository.TransactionManager,
) TransferService {
	return &transferService{
		walletRepo:      walletRepo,
		transferRepo:    transferRepo,
		ledgerRepo:      ledgerRepo,
		idempotencyRepo: idempotencyRepo,
		txManager:       txManager,
	}
}

// CreateTransfer handles wallet-to-wallet transfers: it validates the request,
// fingerprints it and runs the transfer transaction with retries. The
// transaction itself is described in executeTransfer.
func (s *transferService) CreateTransfer(ctx context.Context, req CreateTransferRequest) (*TransferResult, error) {
	transfer := domain.Transfer{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		Status:         domain.TransferPending,
	}
	if err := transfer.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidTransferRequest, err)
	}

	requestHash, err := domain.TransferFingerprint(transfer.FromWalletID, transfer.ToWalletID, transfer.Amount)
	if err != nil {
		return nil, fmt.Errorf("generate request fingerprint: %w", err)
	}

	return s.runWithRetry(ctx, transfer, requestHash)
}

// runWithRetry executes the transfer transaction, retrying the whole
// transaction on deadlocks and serialization failures. An uncertain commit is
// never retried blindly: the saved response is looked up instead.
func (s *transferService) runWithRetry(ctx context.Context, transfer domain.Transfer, requestHash string) (*TransferResult, error) {
	for attempt := 1; ; attempt++ {
		result, err := s.attemptTransfer(ctx, transfer, requestHash)
		if err == nil {
			return result, nil
		}

		if errors.Is(err, repository.ErrCommitUncertain) {
			return s.recoverUncertainCommit(ctx, transfer.IdempotencyKey, requestHash, err)
		}

		if !isRetryableDBError(err) || attempt == maxAttempts {
			return nil, err
		}

		if err := sleepBackoff(ctx, attempt); err != nil {
			return nil, err
		}
	}
}

// attemptTransfer runs one transaction. transfer is passed by value so every
// attempt starts from a clean PENDING transfer without an id.
func (s *transferService) attemptTransfer(ctx context.Context, transfer domain.Transfer, requestHash string) (*TransferResult, error) {
	var result *TransferResult

	err := s.txManager.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var err error
		result, err = s.executeTransfer(ctx, tx, &transfer, requestHash)
		return err
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// recoverUncertainCommit resolves a commit whose outcome is unknown (for
// example the connection dropped during COMMIT) from the stored response.
func (s *transferService) recoverUncertainCommit(ctx context.Context, key, requestHash string, commitErr error) (*TransferResult, error) {
	saved, lookupErr := s.getSavedResponse(ctx, key, requestHash)
	if lookupErr == nil {
		return saved, nil
	}

	// Outcome could not be confirmed.
	return nil, commitErr
}

// getSavedResponse reads the stored response for key in its own transaction.
func (s *transferService) getSavedResponse(ctx context.Context, key, requestHash string) (*TransferResult, error) {
	var result *TransferResult

	err := s.txManager.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var err error
		result, err = s.loadSavedResponse(ctx, tx, key, requestHash)
		return err
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// sleepBackoff waits 100ms, 200ms, ... before the next attempt, or returns the
// context error if the caller gives up first.
func sleepBackoff(ctx context.Context, attempt int) error {
	delay := time.Duration(100*(1<<(attempt-1))) * time.Millisecond

	select {
	case <-time.After(delay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

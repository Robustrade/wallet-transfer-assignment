package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	repository "github.com/Robustrade/wallet-transfer-assignment/internal/respository"
	"github.com/google/uuid"
)

// ErrIdempotencyConflict is returned when an idempotency key is reused with a different request payload.
var ErrIdempotencyConflict = errors.New("idempotency conflict: request hash mismatch")

type TransferRequest struct {
	TransferID     string
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	RequestHash    string
	// Actor metadata for idempotency auditing
	CreatedBy string
	CallerIP  string
	UserAgent string
}

type TransferService struct {
	txManager       repository.TransactionManager
	walletRepo      repository.WalletRepository
	transferRepo    repository.TransferRepository
	ledgerRepo      repository.LedgerRepository
	idempotencyRepo repository.IdempotencyRepository
	metrics         domain.Metrics
}

// TransferByID retrieves a transfer record by its unique identifier.
func (s *TransferService) TransferByID(ctx context.Context, id string) (*domain.Transfer, error) {
	return s.transferRepo.GetByID(ctx, id)
}

func (s *TransferService) Execute(ctx context.Context, req TransferRequest) (*domain.Transfer, error) {
	if s.metrics != nil {
		start := time.Now()
		defer func() {
			s.metrics.ObserveTransferDuration(time.Since(start).Seconds())
		}()
	}
	// Fast path: return existing idempotent result when available.
	if t, err := s.getExistingTransfer(ctx, req); err != nil {
		return nil, err
	} else if t != nil {
		return t, nil
	}

	// Otherwise attempt the transfer with bounded retries on conflict.
	return s.attemptTransferWithRetries(ctx, req)
}

func (s *TransferService) checkIdempotency(ctx context.Context, req TransferRequest) (*domain.Transfer, error) {
	if t, err := s.checkIdempotencyRecord(ctx, req); err != nil {
		return nil, err
	} else if t != nil {
		return t, nil
	}

	return s.getTransferByIdempotencyKey(ctx, req)
}

// checkIdempotencyRecord looks up an idempotency record and returns the associated transfer if found.
func (s *TransferService) checkIdempotencyRecord(ctx context.Context, req TransferRequest) (*domain.Transfer, error) {
	if s.idempotencyRepo == nil {
		return nil, nil
	}

	record, err := s.idempotencyRepo.GetByKey(ctx, req.IdempotencyKey)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}

	if record.RequestHash != req.RequestHash {
		return nil, ErrIdempotencyConflict
	}

	if s.metrics != nil {
		s.metrics.IncIdempotencyHit()
	}

	return s.transferRepo.GetByID(ctx, record.TransferID)
}

// getTransferByIdempotencyKey looks up a transfer by its idempotency key (historical fallback).
func (s *TransferService) getTransferByIdempotencyKey(ctx context.Context, req TransferRequest) (*domain.Transfer, error) {
	transfer, err := s.transferRepo.GetByIdempotencyKey(ctx, req.IdempotencyKey)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}

	if s.metrics != nil {
		s.metrics.IncIdempotencyHit()
	}

	return transfer, nil
}

// getExistingTransfer returns an existing transfer tied to the idempotency key
// or nil when none exists. It encapsulates idempotency-record lookup and the
// historical fallback to the transfers table.
func (s *TransferService) getExistingTransfer(ctx context.Context, req TransferRequest) (*domain.Transfer, error) {
	if s.idempotencyRepo != nil {
		rec, err := s.idempotencyRepo.GetByKey(ctx, req.IdempotencyKey)
		if err == nil {
			if rec.RequestHash != req.RequestHash {
				return nil, ErrIdempotencyConflict
			}
			if s.metrics != nil {
				s.metrics.IncIdempotencyHit()
			}
			return s.transferRepo.GetByID(ctx, rec.TransferID)
		}
		if !errors.Is(err, repository.ErrNotFound) {
			return nil, err
		}
	}

	// Fallback for deployments that may not populate idempotency_records
	tr, err := s.transferRepo.GetByIdempotencyKey(ctx, req.IdempotencyKey)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if s.metrics != nil {
		s.metrics.IncIdempotencyHit()
	}
	return tr, nil
}

func (s *TransferService) attemptTransferWithRetries(ctx context.Context, req TransferRequest) (*domain.Transfer, error) {
	const maxAttempts = 3
	for i := 0; i < maxAttempts; i++ {
		tr, err := s.runTransferTransaction(ctx, req)
		if err == nil {
			return tr, nil
		}
		if !errors.Is(err, repository.ErrConflict) {
			return nil, err
		}

		// If conflict, another concurrent request may have completed; re-check idempotency.
		if doneTr, err := s.getExistingTransfer(ctx, req); err != nil {
			return nil, err
		} else if doneTr != nil {
			return doneTr, nil
		}
	}
	return nil, repository.ErrConflict
}

func (s *TransferService) runTransferTransaction(ctx context.Context, req TransferRequest) (*domain.Transfer, error) {
	var transfer *domain.Transfer
	err := s.txManager.RunInTransaction(ctx, func(txCtx context.Context) error {
		var err error
		transfer, err = s.executeAtomicTransfer(txCtx, req)
		return err
	})
	return transfer, err
}

func (s *TransferService) executeAtomicTransfer(txCtx context.Context, req TransferRequest) (*domain.Transfer, error) {
	if s.ledgerRepo == nil {
		return nil, errors.New("ledger repository is required")
	}
	if s.idempotencyRepo == nil {
		return nil, errors.New("idempotency repository is required")
	}

	// Server-side transfer ID generation: prefer client-provided but generate if empty
	transferID := req.TransferID
	if transferID == "" {
		transferID = uuid.NewString()
	}

	// Pre-claim idempotency key: insert PENDING record to serialize concurrent requests.
	// If the key already exists, handle based on stored request hash / status.
	if s.idempotencyRepo != nil {
		rec := &repository.IdempotencyRecord{
			IdempotencyKey: req.IdempotencyKey,
			TransferID:     transferID,
			RequestHash:    req.RequestHash,
			Status:         repository.IdempotencyStatusPending,
			ResponseJSON:   nil,
			StatusCode:     0,
			CreatedBy:      req.CreatedBy,
			CallerIP:       req.CallerIP,
			UserAgent:      req.UserAgent,
		}
		if err := s.idempotencyRepo.Create(txCtx, rec); err != nil {
			if errors.Is(err, repository.ErrConflict) {
				// Existing claim: examine existing record
				existing, gerr := s.idempotencyRepo.GetByKey(txCtx, req.IdempotencyKey)
				if gerr != nil {
					return nil, gerr
				}
				if existing.RequestHash != req.RequestHash {
					return nil, ErrIdempotencyConflict
				}
				if existing.Status == repository.IdempotencyStatusCompleted {
					return s.transferRepo.GetByID(txCtx, existing.TransferID)
				}
				// Another concurrent request is in-flight; surface conflict so caller can retry/fall back
				return nil, repository.ErrConflict
			}
			return nil, err
		}
	}

	// Lock wallets and identify roles
	fromWallet, toWallet, err := s.lockAndIdentify(txCtx, req)
	if err != nil {
		return nil, err
	}

	if fromWallet.Balance < req.Amount {
		return nil, domain.ErrInsufficientFunds
	}

	fromWallet.Balance -= req.Amount
	toWallet.Balance += req.Amount

	if err := s.walletRepo.Update(txCtx, fromWallet); err != nil {
		return nil, err
	}
	if err := s.walletRepo.Update(txCtx, toWallet); err != nil {
		return nil, err
	}

	// Persist transfer and ledger entries (idempotency record already claimed above)
	transfer, err := s.persistTransferAndIdempotency(txCtx, req, transferID)
	if err != nil {
		// attempt to mark idempotency record as FAILED
		_ = s.idempotencyRepo.UpdateStatus(txCtx, req.IdempotencyKey, repository.IdempotencyStatusFailed)
		return nil, err
	}

	// Store the final response for idempotency replay
	responseJSON, err := json.Marshal(map[string]string{"transfer_id": transfer.ID, "status": string(transfer.Status)})
	if err != nil {
		return nil, err
	}
	if err := s.idempotencyRepo.UpdateResponse(txCtx, req.IdempotencyKey, responseJSON, 200, repository.IdempotencyStatusCompleted); err != nil {
		return nil, err
	}

	return transfer, nil
}

// persistTransferAndIdempotency creates transfer, ledger entries, and idempotency record under the same transaction.
func (s *TransferService) persistTransferAndIdempotency(ctx context.Context, req TransferRequest, transferID string) (*domain.Transfer, error) {
	transfer := &domain.Transfer{
		ID:             transferID,
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		Status:         domain.TransferStatusProcessed,
	}

	if err := s.transferRepo.Create(ctx, transfer); err != nil {
		return nil, err
	}

	if err := s.createLedgerEntries(ctx, req); err != nil {
		return nil, err
	}

	return transfer, nil
}

// lockAndIdentify locks the two wallets in deterministic order and returns the source and destination wallets.
func (s *TransferService) lockAndIdentify(txCtx context.Context, req TransferRequest) (*domain.Wallet, *domain.Wallet, error) {
	firstID, secondID := req.FromWalletID, req.ToWalletID
	if firstID > secondID {
		firstID, secondID = secondID, firstID
	}

	w1, err := s.walletRepo.GetByIDForUpdate(txCtx, firstID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to lock wallet %s: %w", firstID, err)
	}
	w2, err := s.walletRepo.GetByIDForUpdate(txCtx, secondID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to lock wallet %s: %w", secondID, err)
	}

	from, to := w1, w2
	if w1.ID == req.ToWalletID {
		from, to = w2, w1
	}
	return from, to, nil
}

func (s *TransferService) createLedgerEntries(ctx context.Context, req TransferRequest) error {
	debitEntry, err := domain.NewLedgerEntry(uuid.NewString(), req.TransferID, req.FromWalletID, domain.LedgerEntryTypeDebit, req.Amount)
	if err != nil {
		return err
	}
	creditEntry, err := domain.NewLedgerEntry(uuid.NewString(), req.TransferID, req.ToWalletID, domain.LedgerEntryTypeCredit, req.Amount)
	if err != nil {
		return err
	}
	return s.ledgerRepo.CreateEntries(ctx, []domain.LedgerEntry{*debitEntry, *creditEntry})
}

func NewTransferService(tm repository.TransactionManager, wr repository.WalletRepository, tr repository.TransferRepository, lr repository.LedgerRepository, ir repository.IdempotencyRepository, m domain.Metrics) *TransferService {
	return &TransferService{txManager: tm, walletRepo: wr, transferRepo: tr, ledgerRepo: lr, idempotencyRepo: ir, metrics: m}
}

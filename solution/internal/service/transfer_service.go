package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/heisenberglit/wallet-transfer-assignment/internal/domain"
	"github.com/heisenberglit/wallet-transfer-assignment/internal/repository"
)

// CreateTransferInput is the service-layer request for a new transfer.
type CreateTransferInput struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

type TransferService struct {
	wallets   repository.WalletRepository
	transfers repository.TransferRepository
	executor  repository.TransferExecutor
}

func NewTransferService(
	wallets repository.WalletRepository,
	transfers repository.TransferRepository,
	executor repository.TransferExecutor,
) *TransferService {
	return &TransferService{
		wallets:   wallets,
		transfers: transfers,
		executor:  executor,
	}
}

func (s *TransferService) CreateTransfer(ctx context.Context, in CreateTransferInput) (*domain.Transfer, error) {
	fingerprint := domain.RequestFingerprint(in.FromWalletID, in.ToWalletID, in.Amount)

	if existing, err := s.transfers.GetByIdempotencyKey(ctx, in.IdempotencyKey); err != nil {
		return nil, err
	} else if existing != nil {
		slog.Info("transfer idempotency replay", "transfer_id", existing.ID,
			"idempotency_key", in.IdempotencyKey, "state", existing.State)
		return s.resumeOrReplay(ctx, existing, in.IdempotencyKey, fingerprint)
	}

	if in.Amount <= 0 {
		return nil, domain.ErrInvalidAmount
	}
	if in.FromWalletID == in.ToWalletID {
		return nil, domain.ErrSameWallet
	}

	if _, err := s.wallets.Get(ctx, in.FromWalletID); err != nil {
		return nil, err
	}
	if _, err := s.wallets.Get(ctx, in.ToWalletID); err != nil {
		return nil, err
	}

	now := time.Now()
	transfer := &domain.Transfer{
		ID:             uuid.NewString(),
		IdempotencyKey: in.IdempotencyKey,
		RequestHash:    fingerprint,
		FromWalletID:   in.FromWalletID,
		ToWalletID:     in.ToWalletID,
		Amount:         in.Amount,
		State:          domain.TransferPending,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := s.transfers.Create(ctx, transfer); err != nil {
		if errors.Is(err, domain.ErrIdempotencyConflict) {
			existing, getErr := s.transfers.GetByIdempotencyKey(ctx, in.IdempotencyKey)
			if getErr != nil {
				return nil, getErr
			}
			if existing != nil {
				slog.Info("transfer idempotency race lost, returning winner",
					"transfer_id", existing.ID, "idempotency_key", in.IdempotencyKey, "state", existing.State)
				return s.resumeOrReplay(ctx, existing, in.IdempotencyKey, fingerprint)
			}
		}
		slog.Error("transfer create failed", "idempotency_key", in.IdempotencyKey, "error", err)
		return nil, err
	}

	slog.Info("transfer created", "transfer_id", transfer.ID, "idempotency_key", in.IdempotencyKey,
		"from_wallet_id", in.FromWalletID, "to_wallet_id", in.ToWalletID, "amount", in.Amount)

	return s.apply(ctx, transfer, in.IdempotencyKey)
}

// resumeOrReplay answers for an idempotency key that already has a transfer.
// Both the pre-flight lookup and the lost-insert race land here, so they
// cannot answer the same situation differently.
func (s *TransferService) resumeOrReplay(ctx context.Context, existing *domain.Transfer, key, fingerprint string) (*domain.Transfer, error) {
	if existing.RequestHash != fingerprint {
		// Same key, different request. Returning the original would answer for
		// something the caller never asked for, so refuse instead.
		slog.Warn("idempotency key reused with a different payload",
			"transfer_id", existing.ID, "idempotency_key", key)
		return nil, domain.ErrPayloadMismatch
	}
	if existing.State == domain.TransferPending {
		// Inserted but not finished: the original caller crashed, or is still
		// running. Drive it to a terminal state rather than stranding the key.
		// Execute's state CAS makes this safe against a live original.
		slog.Info("resuming a pending transfer", "transfer_id", existing.ID)
		return s.apply(ctx, existing, key)
	}
	return replay(existing)
}

func (s *TransferService) apply(ctx context.Context, transfer *domain.Transfer, key string) (*domain.Transfer, error) {
	now := time.Now()
	debit := domain.LedgerEntry{ID: uuid.NewString(), WalletID: transfer.FromWalletID, TransferID: transfer.ID, Type: domain.LedgerDebit, Amount: transfer.Amount, CreatedAt: now}
	credit := domain.LedgerEntry{ID: uuid.NewString(), WalletID: transfer.ToWalletID, TransferID: transfer.ID, Type: domain.LedgerCredit, Amount: transfer.Amount, CreatedAt: now}

	err := s.executor.Execute(ctx, transfer, debit, credit)
	switch {
	case err == nil:
		slog.Info("transfer processed", "transfer_id", transfer.ID, "idempotency_key", key)
		transfer.State = domain.TransferProcessed
		return transfer, nil

	case errors.Is(err, domain.ErrInsufficientFunds):
		// Execute already committed the FAILED state in the same transaction
		// that declined to move the money, so there is no second write here to
		// lose if this request is cancelled.
		slog.Warn("transfer failed: insufficient funds", "transfer_id", transfer.ID, "idempotency_key", key)
		transfer.State = domain.TransferFailed
		return transfer, domain.ErrInsufficientFunds

	case errors.Is(err, domain.ErrInvalidStateTransition):
		return s.outcomeOf(ctx, key, err)

	default:
		slog.Error("transfer execute failed", "transfer_id", transfer.ID, "error", err)
		return nil, err
	}
}

func (s *TransferService) outcomeOf(ctx context.Context, key string, cause error) (*domain.Transfer, error) {
	current, err := s.transfers.GetByIdempotencyKey(ctx, key)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, cause
	}
	return replay(current)
}

// replay reproduces the original outcome for an already-seen idempotency key,
// error included — only a PROCESSED transfer may be reported as a success.
func replay(existing *domain.Transfer) (*domain.Transfer, error) {
	switch existing.State {
	case domain.TransferFailed:
		return existing, domain.ErrInsufficientFunds
	case domain.TransferPending:
		return existing, domain.ErrTransferInProgress
	default:
		return existing, nil
	}
}

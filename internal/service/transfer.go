package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/ankurnitt1311/wallet-transfer-assignment/internal/domain"
	"github.com/ankurnitt1311/wallet-transfer-assignment/internal/repository"
	"github.com/google/uuid"
)

const (
	maxAttempts      = 16
	busyBackoffBase  = 20 * time.Millisecond
	failInsufficient = "insufficient_funds"
)

type clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type idgen interface {
	New() string
}

type uuidGen struct{}

func (uuidGen) New() string { return uuid.NewString() }

// Service orchestrates wallet transfers. It owns idempotency, state
// transitions, and the transactional workflow. It does not speak HTTP.
type Service struct {
	store *repository.Store
	log   *slog.Logger
	clock clock
	ids   idgen
}

func New(store *repository.Store, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{store: store, log: log, clock: realClock{}, ids: uuidGen{}}
}

type CreateTransferRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

type CreateTransferResult struct {
	Transfer domain.Transfer
	Replay   bool
}

func (s *Service) CreateTransfer(ctx context.Context, req CreateTransferRequest) (CreateTransferResult, error) {
	draft, err := domain.NewTransfer(domain.CreateTransferInput{
		ID:             s.ids.New(),
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		Now:            s.clock.Now(),
	})
	if err != nil {
		return CreateTransferResult{}, err
	}
	hash := domain.RequestHash(req.FromWalletID, req.ToWalletID, req.Amount)

	var result CreateTransferResult
	err = withRetry(ctx, func() error {
		return s.store.WithTx(ctx, func(tx *repository.Tx) error {
			existing, err := tx.GetIdempotency(ctx, req.IdempotencyKey)
			if err == nil {
				if existing.RequestHash != hash {
					return domain.ErrIdempotencyConflict
				}
				tr, err := tx.GetTransfer(ctx, existing.TransferID)
				if err != nil {
					return err
				}
				result = CreateTransferResult{Transfer: tr, Replay: true}
				return nil
			}
			if !errors.Is(err, repository.ErrNotFound) {
				return err
			}

			if _, err := tx.GetWallet(ctx, req.FromWalletID); err != nil {
				return err
			}
			if _, err := tx.GetWallet(ctx, req.ToWalletID); err != nil {
				return err
			}

			now := s.clock.Now()
			if err := s.applyBalances(ctx, tx, req.FromWalletID, req.ToWalletID, req.Amount); err != nil {
				if !errors.Is(err, domain.ErrInsufficientFunds) {
					return err
				}
				failed, err := draft.MarkFailed(failInsufficient, now)
				if err != nil {
					return err
				}
				if err := persistOutcome(ctx, tx, failed, hash, now, nil, nil); err != nil {
					return err
				}
				result = CreateTransferResult{Transfer: failed}
				return nil
			}

			processed, err := draft.MarkProcessed(now)
			if err != nil {
				return err
			}
			debit, credit, err := domain.NewPosting(
				processed.ID,
				processed.FromWalletID,
				processed.ToWalletID,
				s.ids.New(),
				s.ids.New(),
				processed.Amount,
				now,
			)
			if err != nil {
				return err
			}
			if !debit.Balances(credit) {
				return errors.New("unbalanced ledger posting")
			}
			if err := persistOutcome(ctx, tx, processed, hash, now, &debit, &credit); err != nil {
				return err
			}
			result = CreateTransferResult{Transfer: processed}
			return nil
		})
	})
	if err != nil {
		return CreateTransferResult{}, err
	}

	s.log.InfoContext(ctx, "transfer completed",
		slog.String("transfer_id", result.Transfer.ID),
		slog.String("idempotency_key", result.Transfer.IdempotencyKey),
		slog.String("from_wallet_id", result.Transfer.FromWalletID),
		slog.String("to_wallet_id", result.Transfer.ToWalletID),
		slog.Int64("amount", result.Transfer.Amount),
		slog.String("status", string(result.Transfer.Status)),
		slog.Bool("replay", result.Replay),
	)
	return result, nil
}

func persistOutcome(
	ctx context.Context,
	tx *repository.Tx,
	tr domain.Transfer,
	hash string,
	now time.Time,
	debit, credit *domain.LedgerEntry,
) error {
	if err := tx.InsertTransfer(ctx, tr); err != nil {
		return err
	}
	if debit != nil {
		if err := tx.InsertLedgerEntry(ctx, *debit); err != nil {
			return err
		}
	}
	if credit != nil {
		if err := tx.InsertLedgerEntry(ctx, *credit); err != nil {
			return err
		}
	}
	return tx.InsertIdempotency(ctx, domain.IdempotencyRecord{
		IdempotencyKey: tr.IdempotencyKey,
		TransferID:     tr.ID,
		RequestHash:    hash,
		CreatedAt:      now.UTC(),
	})
}

func (s *Service) applyBalances(ctx context.Context, tx *repository.Tx, fromID, toID string, amount int64) error {
	// Lock both rows in sorted id order, then debit before credit.
	// That avoids A→B / B→A deadlocks and never credits if the source is short.
	first, second := fromID, toID
	if toID < fromID {
		first, second = toID, fromID
	}
	if err := tx.Touch(ctx, first); err != nil {
		return err
	}
	if err := tx.Touch(ctx, second); err != nil {
		return err
	}
	if _, err := tx.Debit(ctx, fromID, amount); err != nil {
		return err
	}
	if _, err := tx.Credit(ctx, toID, amount); err != nil {
		return err
	}
	return nil
}

func withRetry(ctx context.Context, fn func() error) error {
	var last error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		last = fn()
		if last == nil {
			return nil
		}
		if repository.IsUniqueViolation(last) {
			// The sibling request won the idempotency key. Retry so the
			// lookup path returns that transfer instead of inserting again.
			continue
		}
		if repository.IsBusy(last) && attempt < maxAttempts {
			delay := busyBackoffBase * time.Duration(attempt)
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			continue
		}
		return last
	}
	return last
}

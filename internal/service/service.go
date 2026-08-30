package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/codejk-crypto/wallet-transfer-assignment/internal/domain"
	"github.com/codejk-crypto/wallet-transfer-assignment/internal/repository"
)

const maxIdempotencyAttempts = 8

type Service struct {
	pool *pgxpool.Pool
	repo *repository.Postgres
	log  *slog.Logger
}

func New(pool *pgxpool.Pool, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{pool: pool, repo: repository.New(pool), log: log}
}

func (s *Service) CreateWallet(ctx context.Context, in domain.CreateWalletInput) (domain.Wallet, error) {
	if err := in.Validate(); err != nil {
		return domain.Wallet{}, err
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		id = uuid.NewString()
	}
	w := domain.Wallet{ID: id, Balance: in.Balance}
	if err := s.repo.InsertWallet(ctx, w); err != nil {
		return domain.Wallet{}, err
	}
	return w, nil
}

func (s *Service) GetWallet(ctx context.Context, id string) (domain.Wallet, error) {
	if id == "" {
		return domain.Wallet{}, domain.ErrWalletNotFound
	}
	return s.repo.GetWallet(ctx, id)
}

func (s *Service) CreateTransfer(ctx context.Context, in domain.CreateTransferInput) (domain.Transfer, int, error) {
	if err := in.Validate(); err != nil {
		return domain.Transfer{}, 0, err
	}
	hash := in.RequestHash()

	var last error
	for attempt := 0; attempt < maxIdempotencyAttempts; attempt++ {
		t, status, err := s.createTransferOnce(ctx, in, hash)
		if err == nil {
			return t, status, nil
		}
		if errors.Is(err, domain.ErrIdempotencyRetry) {
			last = err
			time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
			continue
		}
		return domain.Transfer{}, 0, err
	}
	return domain.Transfer{}, 0, last
}

func (s *Service) createTransferOnce(ctx context.Context, in domain.CreateTransferInput, hash string) (domain.Transfer, int, error) {
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return domain.Transfer{}, 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	claimed, rec, err := repository.ClaimIdempotency(ctx, tx, in.IdempotencyKey, hash)
	if err != nil {
		return domain.Transfer{}, 0, err
	}

	if !claimed {
		if rec.Hash != hash {
			return domain.Transfer{}, 0, domain.ErrIdempotencyConflict
		}
		if rec.Status != domain.IdempotencyCompleted || rec.TransferID == nil {
			return domain.Transfer{}, 0, domain.ErrIdempotencyRetry
		}
		t, err := repository.GetTransfer(ctx, tx, *rec.TransferID)
		if err != nil {
			return domain.Transfer{}, 0, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.Transfer{}, 0, err
		}
		httpStatus := 201
		if rec.HTTPStatus != nil {
			httpStatus = *rec.HTTPStatus
		}
		s.log.Info("idempotent transfer replay", "idempotencyKey", in.IdempotencyKey, "transferId", t.ID, "status", t.Status)
		return t, httpStatus, nil
	}

	firstID, secondID := in.FromWalletID, in.ToWalletID
	if secondID < firstID {
		firstID, secondID = secondID, firstID
	}
	w1, w2, err := repository.LockWallets(ctx, tx, firstID, secondID)
	if err != nil {
		return domain.Transfer{}, 0, err
	}
	fromW := w1
	if w1.ID != in.FromWalletID {
		fromW = w2
	}

	t := domain.Transfer{
		ID:           uuid.NewString(),
		FromWalletID: in.FromWalletID,
		ToWalletID:   in.ToWalletID,
		Amount:       in.Amount,
		Status:       domain.StatusPending,
	}
	if err := repository.InsertTransfer(ctx, tx, t); err != nil {
		return domain.Transfer{}, 0, err
	}

	httpStatus := 201
	if fromW.Balance < in.Amount {
		if err := repository.UpdateTransferStatus(ctx, tx, t.ID, domain.StatusPending, domain.StatusFailed, domain.FailureInsufficientFunds); err != nil {
			return domain.Transfer{}, 0, err
		}
		t.Status = domain.StatusFailed
		t.FailureReason = domain.FailureInsufficientFunds
		httpStatus = 422
	} else {
		debit := domain.LedgerEntry{
			ID: uuid.NewString(), TransferID: t.ID, WalletID: in.FromWalletID, Type: domain.EntryDebit, Amount: in.Amount,
		}
		credit := domain.LedgerEntry{
			ID: uuid.NewString(), TransferID: t.ID, WalletID: in.ToWalletID, Type: domain.EntryCredit, Amount: in.Amount,
		}
		if err := repository.InsertLedgerEntries(ctx, tx, debit, credit); err != nil {
			return domain.Transfer{}, 0, err
		}
		if err := repository.ApplyBalances(ctx, tx, in.FromWalletID, in.ToWalletID, in.Amount); err != nil {
			return domain.Transfer{}, 0, err
		}
		if err := repository.UpdateTransferStatus(ctx, tx, t.ID, domain.StatusPending, domain.StatusProcessed, ""); err != nil {
			return domain.Transfer{}, 0, err
		}
		t.Status = domain.StatusProcessed
	}

	if err := repository.CompleteIdempotency(ctx, tx, in.IdempotencyKey, t.ID, httpStatus); err != nil {
		return domain.Transfer{}, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Transfer{}, 0, err
	}

	s.log.Info("transfer completed",
		"idempotencyKey", in.IdempotencyKey,
		"transferId", t.ID,
		"from", in.FromWalletID,
		"to", in.ToWalletID,
		"amount", in.Amount,
		"status", t.Status,
	)
	return t, httpStatus, nil
}

func (s *Service) Pool() *pgxpool.Pool {
	return s.pool
}

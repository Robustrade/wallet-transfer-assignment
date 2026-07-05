// Package service contains transfer business logic: idempotency handling,
// transfer orchestration, and ledger/balance invariants.
//
// TransferService only wires its repository dependency; the orchestration
// logic that implements the transfer design lives here as it's written.
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/domain"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/repository"
)

// TransferService orchestrates wallet-to-wallet transfers.
type TransferService struct {
	store       *repository.Store
	wallets     *repository.WalletRepository
	idempotency *repository.IdempotencyRepository
	transfer    *repository.TransferRepository
	ledger      *repository.LedgerRepository
}

// NewTransferService constructs a TransferService around a repository Store.
func NewTransferService(store *repository.Store) *TransferService {
	return &TransferService{
		store:       store,
		wallets:     repository.NewWalletRepository(),
		idempotency: repository.NewIdempotencyRepository(),
		transfer:    repository.NewTransferRepository(),
		ledger:      repository.NewLedgerRepository(),
	}
}

type CreateTransferRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

// CreateTransfer creates (or replays) the transfer for the given idempotency
// key and drives it to a terminal state (PROCESSED/FAILED).
func (s *TransferService) CreateTransfer(ctx context.Context, req CreateTransferRequest) (domain.Transfer, error) {
	if req.FromWalletID == "" || req.ToWalletID == "" {
		return domain.Transfer{}, domain.ErrWalletIdRequired
	}
	if req.FromWalletID == req.ToWalletID {
		return domain.Transfer{}, domain.ErrSameWallet
	}
	if req.Amount <= 0 {
		return domain.Transfer{}, domain.ErrInvalidAmount
	}
	if req.IdempotencyKey == "" {
		return domain.Transfer{}, domain.ErrIdempotencyKeyRequired
	}

	if _, err := s.wallets.GetWalletByID(ctx, s.store.Pool, req.FromWalletID); err != nil {
		return domain.Transfer{}, err
	}
	if _, err := s.wallets.GetWalletByID(ctx, s.store.Pool, req.ToWalletID); err != nil {
		return domain.Transfer{}, err
	}

	requestHash := computeRequestHash(req.FromWalletID, req.ToWalletID, req.Amount)

	transferID, err := s.findOrCreateTransfer(ctx, req, requestHash)
	if err != nil {
		return domain.Transfer{}, err
	}

	return s.processTransfer(ctx, transferID)
}

func (s *TransferService) findOrCreateTransfer(ctx context.Context, req CreateTransferRequest, requestHash string) (uuid.UUID, error) {
	record, err := s.idempotency.GetIdempotencyRecord(ctx, s.store.Pool, req.IdempotencyKey)
	if err == nil {
		if record.RequestHash != requestHash {
			return uuid.Nil, domain.ErrIdempotencyKeyConflict
		}
		return record.TransferID, nil
	}
	if !errors.Is(err, domain.ErrIdempotencyRecordNotFound) {
		return uuid.Nil, err
	}

	transferID := uuid.New()
	transfer, err := domain.NewPendingTransfer(transferID, req.FromWalletID, req.ToWalletID, req.Amount)
	if err != nil {
		return uuid.Nil, err
	}

	txErr := s.store.WithinTx(ctx, func(tx pgx.Tx) error {
		if err := s.transfer.InsertTransfer(ctx, tx, transfer); err != nil {
			return err
		}
		return s.idempotency.InsertIdempotencyKey(ctx, tx, domain.IdempotencyRecord{
			IdempotencyKey: req.IdempotencyKey,
			RequestHash:    requestHash,
			TransferID:     transferID,
			CreatedAt:      time.Now().Truncate(time.Microsecond),
		})
	})
	if txErr == nil {
		return transferID, nil
	}
	if !isUniqueViolation(txErr) {
		return uuid.Nil, txErr
	}

	record, err = s.idempotency.GetIdempotencyRecord(ctx, s.store.Pool, req.IdempotencyKey)
	if err != nil {
		return uuid.Nil, err
	}
	if record.RequestHash != requestHash {
		return uuid.Nil, domain.ErrIdempotencyKeyConflict
	}
	return record.TransferID, nil
}

func (s *TransferService) processTransfer(ctx context.Context, transferID uuid.UUID) (domain.Transfer, error) {
	var result domain.Transfer
	err := s.store.WithinTx(ctx, func(tx pgx.Tx) error {
		transfer, err := s.transfer.LockForUpdate(ctx, tx, transferID)
		if err != nil {
			return err
		}
		if transfer.Status != domain.StatusPending {
			result = transfer
			return nil
		}

		wallets, err := s.wallets.LockForUpdate(ctx, tx, []string{transfer.FromWalletID, transfer.ToWalletID})
		if err != nil {
			return err
		}
		fromWallet := wallets[transfer.FromWalletID]
		toWallet := wallets[transfer.ToWalletID]

		if !fromWallet.HasSufficientFunds(transfer.Amount) {
			if err := transfer.MarkFailed("insufficient balance"); err != nil {
				return err
			}
			if err := s.transfer.UpdateTransferStatus(ctx, tx, transfer.ID, transfer.Status, transfer.FailureReason); err != nil {
				return err
			}
			result = transfer
			return nil
		}

		if err := s.wallets.UpdateBalance(ctx, tx, fromWallet.ID, fromWallet.Balance-transfer.Amount); err != nil {
			return err
		}
		if err := s.wallets.UpdateBalance(ctx, tx, toWallet.ID, toWallet.Balance+transfer.Amount); err != nil {
			return err
		}

		debit, credit := domain.NewLedgerPair(transfer.ID, transfer.FromWalletID, transfer.ToWalletID, transfer.Amount)
		if err := s.ledger.InsertLedgerEntries(ctx, tx, debit, credit); err != nil {
			return err
		}

		if err := transfer.MarkProcessed(); err != nil {
			return err
		}
		if err := s.transfer.UpdateTransferStatus(ctx, tx, transfer.ID, transfer.Status, transfer.FailureReason); err != nil {
			return err
		}
		result = transfer
		return nil
	})
	if err != nil {
		return domain.Transfer{}, err
	}
	return result, nil
}

func computeRequestHash(fromWalletID, toWalletID string, amount int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s,%d:%s,%d", len(fromWalletID), fromWalletID, len(toWalletID), toWalletID, amount)))
	return hex.EncodeToString(sum[:])
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

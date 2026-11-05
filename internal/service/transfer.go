// Package service holds the business workflow: transaction boundaries, idempotency
// and the transfer state machine. It calls repositories but writes no SQL itself.
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"

	"github.com/Robustrade/wallet-transfer-assignment/internal/db"
	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
)

// TransferResult is what CreateTransfer returns.
type TransferResult struct {
	Transfer domain.Transfer
	// Replayed is true when this call returned the result of an earlier request with the same key.
	Replayed bool
}

// TransferService creates and executes transfers exactly once per idempotency key.
type TransferService struct {
	db          *sql.DB
	log         *slog.Logger
	wallets     repository.WalletRepository
	transfers   repository.TransferRepository
	ledger      repository.LedgerRepository
	idempotency repository.IdempotencyRepository
}

// NewTransferService builds the service. A nil logger discards logs.
func NewTransferService(conn *sql.DB, log *slog.Logger) *TransferService {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &TransferService{db: conn, log: log}
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// errKeyAlreadyReserved rolls back the reservation transaction when another request won.
var errKeyAlreadyReserved = errors.New("idempotency key already reserved")

// CreateTransfer runs the transfer in two steps:
//
//	Step A (reserve): store the key and a PENDING transfer, committed on its own.
//	Step B (process): lock, move money, write the ledger, finish the state machine.
//
// Step B is safe to run many times for the same transfer, so retries, concurrent
// duplicates and a crash between the steps all end with one single result.
func (s *TransferService) CreateTransfer(ctx context.Context, cmd domain.TransferCommand) (TransferResult, error) {
	if err := cmd.Validate(); err != nil {
		return TransferResult{}, err
	}
	requestHash := HashTransferRequest(cmd)

	transferID, replayed, err := s.reserve(ctx, cmd, requestHash)
	if err != nil {
		return TransferResult{}, err
	}
	transfer, err := s.process(ctx, transferID)
	if err != nil {
		return TransferResult{}, err
	}

	if replayed {
		s.log.InfoContext(ctx, "idempotent replay of transfer",
			"idempotencyKey", cmd.IdempotencyKey, "transferId", transferID)
	}
	return TransferResult{Transfer: transfer, Replayed: replayed}, nil
}

// GetTransfer returns one transfer.
func (s *TransferService) GetTransfer(ctx context.Context, id string) (domain.Transfer, error) {
	if !uuidPattern.MatchString(id) {
		return domain.Transfer{}, fmt.Errorf("%w: %s", domain.ErrTransferNotFound, id)
	}
	t, err := s.transfers.FindByID(ctx, s.db, id)
	if err != nil {
		return domain.Transfer{}, err
	}
	if t == nil {
		return domain.Transfer{}, fmt.Errorf("%w: %s", domain.ErrTransferNotFound, id)
	}
	return *t, nil
}

// ---- Step A ---------------------------------------------------------------------------

func (s *TransferService) reserve(ctx context.Context, cmd domain.TransferCommand, requestHash string) (string, bool, error) {
	// Fast path: most retries find the key already committed.
	if id, err := s.findReservation(ctx, cmd.IdempotencyKey, requestHash); err != nil || id != "" {
		return id, id != "", err
	}

	transferID, err := newUUID()
	if err != nil {
		return "", false, err
	}

	err = db.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.assertWalletsExist(ctx, tx, cmd.FromWalletID, cmd.ToWalletID); err != nil {
			return err
		}
		if _, err := s.transfers.InsertPending(ctx, tx, transferID,
			cmd.FromWalletID, cmd.ToWalletID, cmd.Amount); err != nil {
			return err
		}
		inserted, err := s.idempotency.TryInsert(ctx, tx, cmd.IdempotencyKey, requestHash, transferID)
		if err != nil {
			return err
		}
		if !inserted {
			// Another request with this key won the race. Roll back our PENDING row.
			return errKeyAlreadyReserved
		}
		return nil
	})

	switch {
	case errors.Is(err, errKeyAlreadyReserved):
		winner, err := s.findReservation(ctx, cmd.IdempotencyKey, requestHash)
		if err != nil {
			return "", false, err
		}
		if winner == "" {
			return "", false, errors.New("idempotency record missing after key conflict")
		}
		return winner, true, nil
	case err != nil:
		return "", false, err
	}

	s.log.InfoContext(ctx, "transfer reserved",
		"idempotencyKey", cmd.IdempotencyKey, "transferId", transferID, "amount", cmd.Amount)
	return transferID, false, nil
}

// findReservation returns the transfer id stored for the key, or "" if the key is new.
// It fails with ErrIdempotencyKeyReused if the key was used with a different request.
func (s *TransferService) findReservation(ctx context.Context, key, requestHash string) (string, error) {
	record, err := s.idempotency.FindByKey(ctx, s.db, key)
	if err != nil || record == nil {
		return "", err
	}
	if record.RequestHash != requestHash {
		return "", fmt.Errorf("%w: %s", domain.ErrIdempotencyKeyReused, key)
	}
	return record.TransferID, nil
}

func (s *TransferService) assertWalletsExist(ctx context.Context, q db.Querier, ids ...string) error {
	found, err := s.wallets.FindByIDs(ctx, q, ids)
	if err != nil {
		return err
	}
	exists := make(map[string]bool, len(found))
	for _, w := range found {
		exists[w.ID] = true
	}
	for _, id := range ids {
		if !exists[id] {
			return fmt.Errorf("%w: %s", domain.ErrWalletNotFound, id)
		}
	}
	return nil
}

// ---- Step B ---------------------------------------------------------------------------

// process finishes a PENDING transfer, or returns the stored result if it is already final.
// The value returned is always the decoded snapshot, so the first call and every replay
// return exactly the same data.
func (s *TransferService) process(ctx context.Context, transferID string) (domain.Transfer, error) {
	var snapshot []byte

	err := db.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		// Lock the transfer first: concurrent duplicates queue here, and the
		// second one sees the final state written by the first.
		transfer, err := s.transfers.LockByID(ctx, tx, transferID)
		if err != nil {
			return err
		}
		if transfer == nil {
			return fmt.Errorf("%w: %s", domain.ErrTransferNotFound, transferID)
		}

		if transfer.Status.IsFinal() {
			record, err := s.idempotency.FindByTransferID(ctx, tx, transferID)
			if err != nil {
				return err
			}
			if record != nil && record.Result != nil {
				snapshot = record.Result
				return nil
			}
			snapshot, err = json.Marshal(transfer)
			return err
		}

		finished, err := s.execute(ctx, tx, *transfer)
		if err != nil {
			return err
		}
		if snapshot, err = json.Marshal(finished); err != nil {
			return fmt.Errorf("encode result: %w", err)
		}
		if err := s.idempotency.SaveResult(ctx, tx, transferID, snapshot); err != nil {
			return err
		}

		attrs := []any{"transferId", transferID, "status", finished.Status}
		if finished.FailureReason != nil {
			attrs = append(attrs, "failureReason", *finished.FailureReason)
		}
		s.log.InfoContext(ctx, "transfer finished", attrs...)
		return nil
	})
	if err != nil {
		return domain.Transfer{}, err
	}

	var result domain.Transfer
	if err := json.Unmarshal(snapshot, &result); err != nil {
		return domain.Transfer{}, fmt.Errorf("decode result: %w", err)
	}
	return result, nil
}

// execute moves the money (or fails the transfer) while holding row locks on both wallets.
func (s *TransferService) execute(ctx context.Context, tx *sql.Tx, t domain.Transfer) (*domain.Transfer, error) {
	wallets, err := s.wallets.LockByIDs(ctx, tx, []string{t.FromWalletID, t.ToWalletID})
	if err != nil {
		return nil, err
	}
	var source *domain.Wallet
	for i := range wallets {
		if wallets[i].ID == t.FromWalletID {
			source = &wallets[i]
		}
	}
	if source == nil || len(wallets) != 2 {
		return nil, fmt.Errorf("%w: wallets of transfer %s", domain.ErrWalletNotFound, t.ID)
	}

	if source.Balance < t.Amount {
		if err := domain.AssertTransition(t.Status, domain.StatusFailed); err != nil {
			return nil, err
		}
		reason := domain.ReasonInsufficientFunds
		return s.finish(s.transfers.UpdateStatusIfPending(ctx, tx, t.ID, domain.StatusFailed, &reason))
	}

	if err := domain.AssertTransition(t.Status, domain.StatusProcessed); err != nil {
		return nil, err
	}
	if err := s.wallets.AdjustBalance(ctx, tx, t.FromWalletID, -t.Amount); err != nil {
		return nil, err
	}
	if err := s.wallets.AdjustBalance(ctx, tx, t.ToWalletID, t.Amount); err != nil {
		return nil, err
	}
	if err := s.ledger.InsertPair(ctx, tx, t); err != nil {
		return nil, err
	}
	return s.finish(s.transfers.UpdateStatusIfPending(ctx, tx, t.ID, domain.StatusProcessed, nil))
}

func (s *TransferService) finish(updated *domain.Transfer, err error) (*domain.Transfer, error) {
	if err != nil {
		return nil, err
	}
	if updated == nil {
		// Cannot happen while we hold the row lock; if it does, roll back rather than guess.
		return nil, errors.New("transfer was no longer PENDING while locked")
	}
	return updated, nil
}

package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/AbhishekBadgujar/wallet-transfer-assignment/internals/models"
	"github.com/AbhishekBadgujar/wallet-transfer-assignment/internals/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	// ErrIdempotencyKeyConflict means the same key was reused with a different payload.
	ErrIdempotencyKeyConflict = errors.New("idempotency key reused with a different payload")
)

// CreateTransferInput is the service-layer request shape, decoupled from the
// HTTP JSON shape so the handler can evolve independently.
type CreateTransferInput struct {
	IdempotencyKey string
	FromWalletID   uuid.UUID
	ToWalletID     uuid.UUID
	Amount         int64
}

// TransferResult is what the service returns — enough for the handler to
// build an HTTP response, and also what gets cached as the idempotent replay.
type TransferResult struct {
	TransferID    uuid.UUID            `json:"transferId"`
	State         models.TransferState `json:"state"`
	FromWalletID  uuid.UUID            `json:"fromWalletId"`
	ToWalletID    uuid.UUID            `json:"toWalletId"`
	Amount        int64                `json:"amount"`
	FailureReason string               `json:"failureReason,omitempty"`
}

type TransferService struct {
	txManager    repository.TxManager
	walletRepo   repository.WalletRepository
	transferRepo repository.TransferRepository
	ledgerRepo   repository.LedgerRepository
	idempRepo    repository.IdempotencyRepository
}

func NewTransferService(
	txManager repository.TxManager,
	walletRepo repository.WalletRepository,
	transferRepo repository.TransferRepository,
	ledgerRepo repository.LedgerRepository,
	idempRepo repository.IdempotencyRepository,
) *TransferService {
	return &TransferService{
		txManager:    txManager,
		walletRepo:   walletRepo,
		transferRepo: transferRepo,
		ledgerRepo:   ledgerRepo,
		idempRepo:    idempRepo,
	}
}

// CreateTransfer is the main entry point: idempotent, concurrency-safe
// wallet-to-wallet transfer.
func (s *TransferService) CreateTransfer(ctx context.Context, in CreateTransferInput) (*TransferResult, error) {
	// Create a SHA--256 and request body hash
	requestHash := hashRequest(in)

	// Step 1: idempotency check within the transaction limit
	existing, err := s.lookupIdempotencyRecord(ctx, in.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		// Check if payload is the same, if not return conflict error
		if existing.RequestHash != requestHash {
			return nil, ErrIdempotencyKeyConflict
		}
		var result TransferResult
		if err := json.Unmarshal(existing.ResponseBody, &result); err != nil {
			return nil, fmt.Errorf("unmarshalling cached idempotent response: %w", err)
		}
		return &result, nil
	}

	// Step 2: build the domain transfer (validates amount, distinct wallets, etc)
	transfer, err := models.NewTransfer(in.FromWalletID, in.ToWalletID, in.Amount, in.IdempotencyKey)
	if err != nil {
		return nil, err
	}

	var result *TransferResult

	txErr := s.txManager.WithinTx(ctx, func(tx pgx.Tx) error {
		// Lock both wallets in a consistent order (ascending ID) regardless
		// of transfer direction, to avoid deadlocks when two transfers move
		// money between the same pair of wallets in opposite directions.
		//
		// This MUST happen before inserting the transfer row below. That
		// insert has foreign keys into wallets, and Postgres enforces those
		// by taking an implicit FOR KEY SHARE lock on both referenced wallet
		// rows. If we insert first, every concurrent transfer touching the
		// same wallet pair ends up holding that shared FK lock and then
		// racing the others to upgrade to this FOR UPDATE — each blocked on
		// the others' shared lock — which Postgres reports as a genuine
		// deadlock (SQLSTATE 40P01) and resolves by aborting one of them.
		// Taking FOR UPDATE first avoids the upgrade race entirely: a
		// transaction never blocks on a lock it already holds, so the FK
		// check on the insert is satisfied for free.
		firstID, secondID := orderedIDs(in.FromWalletID, in.ToWalletID)
		firstWallet, err := s.walletRepo.GetForUpdate(ctx, tx, firstID)
		if err != nil {
			return fmt.Errorf("locking wallets: %w", err)
		}
		secondWallet, err := s.walletRepo.GetForUpdate(ctx, tx, secondID)
		if err != nil {
			return fmt.Errorf("locking wallets: %w", err)
		}

		if err := s.transferRepo.Create(ctx, tx, transfer); err != nil {
			// Check unique constraint violatiion on DB Side
			if isUniqueViolation(err) {

				return s.handleRaceLostToDuplicate(ctx, tx, in.IdempotencyKey, &result)
			}
			return fmt.Errorf("creating transfer: %w", err)
		}

		// Map back to from/to regardless of lock order.
		fromWallet, toWallet := firstWallet, secondWallet
		if fromWallet.ID != in.FromWalletID {
			fromWallet, toWallet = secondWallet, firstWallet
		}

		if err := fromWallet.Debit(in.Amount); err != nil {
			return s.failTransfer(ctx, tx, transfer, err, &result)
		}
		if err := toWallet.Credit(in.Amount); err != nil {
			return s.failTransfer(ctx, tx, transfer, err, &result)
		}

		if err := s.walletRepo.UpdateBalance(ctx, tx, fromWallet); err != nil {
			return fmt.Errorf("persisting debit: %w", err)
		}
		if err := s.walletRepo.UpdateBalance(ctx, tx, toWallet); err != nil {
			return fmt.Errorf("persisting credit: %w", err)
		}

		debit, credit := models.NewLedgerPair(transfer.ID, in.FromWalletID, in.ToWalletID, in.Amount)
		if err := s.ledgerRepo.InsertPair(ctx, tx, debit, credit); err != nil {
			return fmt.Errorf("writing ledger: %w", err)
		}

		if err := transfer.MarkProcessed(); err != nil {
			return fmt.Errorf("marking transfer processed: %w", err)
		}
		if err := s.transferRepo.UpdateState(ctx, tx, transfer); err != nil {
			return fmt.Errorf("persisting transfer state: %w", err)
		}

		result = toResult(transfer)
		return s.saveIdempotencyRecord(ctx, tx, in.IdempotencyKey, requestHash, transfer.ID, result)
	})

	if txErr != nil {
		return nil, txErr
	}
	return result, nil
}

// failTransfer marks the transfer FAILED with the given cause and persists
// it, then saves the idempotency record so a retry replays the failure
// instead of re-attempting (a FAILED transfer is a terminal, cacheable
// outcome — same as PROCESSED).
func (s *TransferService) failTransfer(ctx context.Context, tx pgx.Tx, transfer *models.Transfer, cause error, result **TransferResult) error {
	if markErr := transfer.MarkFailed(cause.Error()); markErr != nil {
		return fmt.Errorf("marking transfer failed: %w", markErr)
	}
	if err := s.transferRepo.UpdateState(ctx, tx, transfer); err != nil {
		return fmt.Errorf("persisting failed transfer state: %w", err)
	}

	*result = toResult(transfer)
	requestHash := hashRequest(CreateTransferInput{
		IdempotencyKey: transfer.IdempotencyKey,
		FromWalletID:   transfer.FromWalletID,
		ToWalletID:     transfer.ToWalletID,
		Amount:         transfer.Amount,
	})
	return s.saveIdempotencyRecord(ctx, tx, transfer.IdempotencyKey, requestHash, transfer.ID, *result)
}

// handleRaceLostToDuplicate handles the rare case where this request's
// transfer insert lost a unique-constraint race to a concurrent identical
// request. Rather than erroring, we look up what the winner produced and
// return that as our own result — the caller sees a consistent outcome
// either way.
func (s *TransferService) handleRaceLostToDuplicate(ctx context.Context, tx pgx.Tx, key string, result **TransferResult) error {
	existing, err := s.transferRepo.GetByIdempotencyKey(ctx, tx, key)
	if err != nil {
		return fmt.Errorf("resolving concurrent duplicate transfer: %w", err)
	}
	if existing == nil {
		return errors.New("unique violation on idempotency key but no row found — unexpected state")
	}
	*result = toResult(existing)
	return nil
}

func (s *TransferService) lookupIdempotencyRecord(ctx context.Context, key string) (*repository.IdempotencyRecord, error) {
	var rec *repository.IdempotencyRecord
	err := s.txManager.WithinTx(ctx, func(tx pgx.Tx) error {
		r, err := s.idempRepo.Get(ctx, tx, key)
		if err != nil {
			return err
		}
		rec = r
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("checking idempotency record: %w", err)
	}
	return rec, nil
}

func (s *TransferService) saveIdempotencyRecord(ctx context.Context, tx pgx.Tx, key, requestHash string, transferID uuid.UUID, result *TransferResult) error {
	body, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshalling idempotent response: %w", err)
	}
	statusCode := 201
	if result.State == models.TransferFailed {
		statusCode = 422
	}
	return s.idempRepo.Save(ctx, tx, &repository.IdempotencyRecord{
		IdempotencyKey: key,
		RequestHash:    requestHash,
		TransferID:     transferID,
		ResponseBody:   body,
		StatusCode:     statusCode,
	})
}

func toResult(t *models.Transfer) *TransferResult {
	return &TransferResult{
		TransferID:    t.ID,
		State:         t.State,
		FromWalletID:  t.FromWalletID,
		ToWalletID:    t.ToWalletID,
		Amount:        t.Amount,
		FailureReason: t.FailureReason,
	}
}

// orderedIDs returns the two IDs in a deterministic ascending order, used to
// decide wallet lock acquisition order and prevent deadlocks.
func orderedIDs(a, b uuid.UUID) (first, second uuid.UUID) {
	if a.String() < b.String() {
		return a, b
	}
	return b, a
}

// hashRequest produces a stable hash of the request payload so a repeated
// idempotency key can be checked against the original payload — reuse with
// a different payload is a client error, not a valid replay.
func hashRequest(in CreateTransferInput) string {
	raw := fmt.Sprintf("%s|%s|%d", in.FromWalletID, in.ToWalletID, in.Amount)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// isUniqueViolation checks whether an error is a Postgres unique constraint
// violation or not, used to detect the idempotency-key race.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

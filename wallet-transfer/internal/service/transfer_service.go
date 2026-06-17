// Package service contains the transfer workflow: the transaction boundary,
// the idempotency protocol, deterministic lock ordering, the funds check, the
// guarded state transition, and response assembly. It calls only repository
// methods, never raw SQL.
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/example/wallet-transfer/internal/domain"
	"github.com/example/wallet-transfer/internal/repository"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrProcessing signals a duplicate that arrived while the original is still in
// flight (a defensive case in the single-transaction design).
var ErrProcessing = errors.New("request is still processing")

// Command is the validated input to a transfer.
type Command struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         domain.Amount
}

// Result is the assembled outcome: an HTTP status and a JSON body, ready for the
// handler to write. Replayed is true when it came from a cached idempotent result.
type Result struct {
	HTTPStatus int
	Body       []byte
	Replayed   bool
}

// response is the wire shape returned to clients.
type response struct {
	TransferID    string                `json:"transferId"`
	Status        domain.TransferStatus `json:"status"`
	FromWalletID  string                `json:"fromWalletId"`
	ToWalletID    string                `json:"toWalletId"`
	Amount        domain.Amount         `json:"amount"`
	FailureReason string                `json:"failureReason,omitempty"`
}

// TransferService orchestrates transfers. It owns the connection pool and thus
// the transaction boundary.
type TransferService struct {
	pool *pgxpool.Pool
	repo *repository.Repository
}

func NewTransferService(pool *pgxpool.Pool, repo *repository.Repository) *TransferService {
	return &TransferService{pool: pool, repo: repo}
}

// requestHash binds an idempotency key to the exact payload it was first used
// with, so reuse with a different payload can be rejected.
func requestHash(c Command) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", c.FromWalletID, c.ToWalletID, c.Amount)))
	return hex.EncodeToString(sum[:])
}

// CreateTransfer executes one transfer with exactly-once-effect semantics.
// The entire operation runs in a single transaction: either everything commits
// (including a recorded FAILED outcome) or, on an infrastructure error,
// everything rolls back and the idempotency key is left free to retry.
func (s *TransferService) CreateTransfer(ctx context.Context, c Command) (Result, error) {
	hash := requestHash(c)

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback(ctx) // no-op after a successful Commit

	// 1. Claim the idempotency key BEFORE touching wallets.
	claimed, err := s.repo.ClaimIdempotencyKey(ctx, tx, c.IdempotencyKey, hash)
	if err != nil {
		return Result{}, err
	}
	if !claimed {
		return s.handleDuplicate(ctx, tx, c.IdempotencyKey, hash)
	}

	// 2. Lock both wallets in deterministic order to avoid deadlocks.
	ids := sortedPair(c.FromWalletID, c.ToWalletID)
	wallets, err := s.repo.LockWallets(ctx, tx, ids)
	if err != nil {
		return Result{}, err
	}
	source, ok := wallets[c.FromWalletID]
	if !ok {
		return Result{}, domain.ErrWalletNotFound
	}
	if _, ok := wallets[c.ToWalletID]; !ok {
		return Result{}, domain.ErrWalletNotFound
	}

	// 3. Record the transfer as PENDING.
	transferID, err := s.repo.InsertTransfer(ctx, tx, c.FromWalletID, c.ToWalletID, c.Amount)
	if err != nil {
		return Result{}, err
	}

	// 4a. Business failure: insufficient funds -> record FAILED and commit.
	if source.Balance < c.Amount {
		if _, err := s.repo.MarkFailed(ctx, tx, transferID, "insufficient_funds"); err != nil {
			return Result{}, err
		}
		return s.finishAndCommit(ctx, tx, c, transferID, domain.StatusFailed, "insufficient_funds", 422)
	}

	// 4b. Success path: move money, write the two ledger entries.
	if err := s.repo.AdjustBalance(ctx, tx, c.FromWalletID, -c.Amount); err != nil {
		return Result{}, err
	}
	if err := s.repo.AdjustBalance(ctx, tx, c.ToWalletID, c.Amount); err != nil {
		return Result{}, err
	}
	if err := s.repo.InsertLedgerEntry(ctx, tx, transferID, c.FromWalletID, domain.EntryDebit, c.Amount); err != nil {
		return Result{}, err
	}
	if err := s.repo.InsertLedgerEntry(ctx, tx, transferID, c.ToWalletID, domain.EntryCredit, c.Amount); err != nil {
		return Result{}, err
	}

	// 5. Guarded transition PENDING -> PROCESSED.
	moved, err := s.repo.MarkProcessed(ctx, tx, transferID)
	if err != nil {
		return Result{}, err
	}
	if !moved {
		return Result{}, fmt.Errorf("transfer %s was not in PENDING when finalizing", transferID)
	}

	return s.finishAndCommit(ctx, tx, c, transferID, domain.StatusProcessed, "", 201)
}

// finishAndCommit builds the response, stores it on the idempotency record for
// future replay, commits, and returns the result.
func (s *TransferService) finishAndCommit(ctx context.Context, tx pgx.Tx, c Command, transferID string, status domain.TransferStatus, failureReason string, httpStatus int) (Result, error) {
	body, err := json.Marshal(response{
		TransferID:    transferID,
		Status:        status,
		FromWalletID:  c.FromWalletID,
		ToWalletID:    c.ToWalletID,
		Amount:        c.Amount,
		FailureReason: failureReason,
	})
	if err != nil {
		return Result{}, err
	}
	if err := s.repo.FinalizeIdempotencyRecord(ctx, tx, c.IdempotencyKey, transferID, httpStatus, body); err != nil {
		return Result{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	return Result{HTTPStatus: httpStatus, Body: body}, nil
}

// handleDuplicate resolves a request whose idempotency key already exists.
func (s *TransferService) handleDuplicate(ctx context.Context, tx pgx.Tx, key, hash string) (Result, error) {
	rec, err := s.repo.GetIdempotencyRecordForUpdate(ctx, tx, key)
	if err != nil {
		return Result{}, err
	}
	if rec.RequestHash != hash {
		return Result{}, domain.ErrIdempotencyConflict
	}
	if rec.Status == repository.IdempDone {
		// Replay the original stored response verbatim.
		if err := tx.Commit(ctx); err != nil {
			return Result{}, err
		}
		return Result{HTTPStatus: rec.ResponseStatus, Body: rec.ResponseBody, Replayed: true}, nil
	}
	// Defensive: original still in flight.
	return Result{}, ErrProcessing
}

// sortedPair returns the two ids in ascending order for deterministic locking.
func sortedPair(a, b string) []string {
	if a <= b {
		return []string{a, b}
	}
	return []string{b, a}
}

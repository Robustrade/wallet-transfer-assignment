package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"wallettransfer/internal/domain"
)

// TransferResult is what the service returns to the handler layer. It is
// deliberately transport-agnostic (no HTTP status codes baked in beyond a
// suggested one), so the same result shape is used whether this is a fresh
// transfer or a replayed idempotent response.
type TransferResult struct {
	Transfer domain.Transfer
	Replayed bool // true if this result came from a previous request, not this call
}

// TransferService implements the wallet-transfer use case described in the
// assignment: idempotent, double-entry, concurrency-safe transfers.
type TransferService struct {
	db        TxBeginner
	wallets   WalletRepo
	transfers TransferRepo
	ledger    LedgerRepo
	idem      IdempotencyRepo
	log       *slog.Logger
	newID     func() string // injected for deterministic tests
}

func NewTransferService(
	db TxBeginner,
	wallets WalletRepo,
	transfers TransferRepo,
	ledger LedgerRepo,
	idem IdempotencyRepo,
	log *slog.Logger,
) *TransferService {
	if log == nil {
		log = slog.Default()
	}
	return &TransferService{
		db: db, wallets: wallets, transfers: transfers, ledger: ledger, idem: idem,
		log: log, newID: func() string { return uuid.NewString() },
	}
}

// apiResponse mirrors what the HTTP layer serializes, used purely so we can
// store and replay byte-identical idempotent responses. Keeping this in the
// service package (rather than reusing the domain.Transfer directly) means
// the stored payload is decoupled from internal field renames.
type apiResponse struct {
	TransferID string `json:"transferId"`
	Status     string `json:"status"`
	FromWallet string `json:"fromWalletId"`
	ToWallet   string `json:"toWalletId"`
	Amount     int64  `json:"amount"`
	Reason     string `json:"failureReason,omitempty"`
}

// CreateTransfer executes (or replays) a wallet-to-wallet transfer.
//
// Concurrency & consistency strategy:
//
//  1. The idempotency key is claimed inside the same DB transaction as the
//     balance mutation, using a row lock (SELECT ... FOR UPDATE inside
//     INSERT ... ON CONFLICT). If a second, concurrent request arrives with
//     the same key, it blocks on that row lock until the first request's
//     transaction commits or rolls back, then observes the COMPLETED
//     record and replays it. This gives exactly-once semantics even under
//     true concurrent duplicate delivery, not just sequential retries.
//
//  2. Both wallets are locked with SELECT ... FOR UPDATE, always acquired
//     in ascending wallet-ID order regardless of transfer direction. This
//     total lock ordering prevents the classic deadlock where transfer A
//     (wallet1 -> wallet2) and transfer B (wallet2 -> wallet1) run
//     concurrently and each waits on the row the other holds.
//
//  3. The balance check, debit, credit, ledger inserts, transfer status
//     update, and idempotency completion all happen in one transaction, so
//     a crash or error at any point rolls back the entire operation —
//     there is no window where money leaves one wallet without landing in
//     the other, and no window where a transfer is marked PROCESSED
//     without matching ledger entries.
func (s *TransferService) CreateTransfer(ctx context.Context, req domain.NewTransferRequest) (TransferResult, error) {
	if err := req.Validate(); err != nil {
		return TransferResult{}, err
	}

	fingerprint := fingerprintRequest(req)

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return TransferResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op if already committed

	rec, claimed, err := s.idem.Claim(ctx, tx, req.IdempotencyKey, fingerprint)
	if err != nil {
		return TransferResult{}, fmt.Errorf("claim idempotency key: %w", err)
	}

	if !claimed {
		if rec.RequestFingerprint != fingerprint {
			return TransferResult{}, domain.ErrIdempotencyConflict
		}
		// Another request with this exact key+payload already completed
		// (Claim's row lock guarantees we only get here after it committed).
		var resp apiResponse
		if err := json.Unmarshal(rec.ResponseBody, &resp); err != nil {
			return TransferResult{}, fmt.Errorf("decode stored idempotent response: %w", err)
		}
		replayedTransfer := domain.Transfer{
			ID: resp.TransferID, IdempotencyKey: req.IdempotencyKey,
			FromWalletID: resp.FromWallet, ToWalletID: resp.ToWallet,
			Amount: resp.Amount, Status: domain.TransferStatus(resp.Status),
			FailureReason: resp.Reason,
		}
		// A replayed request must reproduce the ORIGINAL outcome, including
		// a prior failure — otherwise a client retrying a failed transfer
		// would silently see "success" on retry despite nothing new having
		// happened. Map the stored failure reason back to its sentinel
		// error where possible so callers can still use errors.Is.
		if replayedTransfer.Status == domain.TransferFailed {
			return TransferResult{}, replayError(resp.Reason)
		}
		return TransferResult{Transfer: replayedTransfer, Replayed: true}, nil
	}

	transferID := s.newID()

	// Lock wallets BEFORE inserting the transfer row, in a fixed order
	// (ascending ID) so two transfers moving money in opposite directions
	// can never deadlock on each other.
	//
	// This ordering also sidesteps a subtler deadlock: transfers.from/to
	// _wallet_id are foreign keys into wallets, so INSERTing a transfer row
	// makes Postgres implicitly take a FOR KEY SHARE lock on both
	// referenced wallet rows to enforce referential integrity. If that
	// insert happened before our explicit FOR UPDATE locks, many
	// concurrent transactions could each be holding a FOR KEY SHARE lock
	// on the same hot wallet (shared locks are mutually compatible) while
	// all simultaneously trying to upgrade to FOR UPDATE — a classic
	// lock-upgrade deadlock. By locking the wallets ourselves first, the
	// later FK check lock is just our own transaction re-affirming a lock
	// it already holds exclusively, which never conflicts with itself.
	firstID, secondID := req.FromWalletID, req.ToWalletID
	if secondID < firstID {
		firstID, secondID = secondID, firstID
	}
	locked := make(map[string]domain.Wallet, 2)
	for _, id := range []string{firstID, secondID} {
		w, err := s.wallets.LockForUpdate(ctx, tx, id)
		if err != nil {
			if errors.Is(err, domain.ErrWalletNotFound) {
				// Neither wallet row is guaranteed to exist here, so we
				// cannot insert a transfers row (it has NOT NULL foreign
				// keys to wallets) — there is nothing valid to record.
				// The idempotency record alone captures "this key was
				// tried and failed validation" for replay purposes.
				return s.failWithoutTransfer(ctx, tx, req, domain.ErrWalletNotFound)
			}
			return TransferResult{}, fmt.Errorf("lock wallet %s: %w", id, err)
		}
		locked[id] = w
	}
	fromWallet, toWallet := locked[req.FromWalletID], locked[req.ToWalletID]

	// Both wallets are confirmed to exist and are locked by this
	// transaction, so it's now safe to insert the transfer row — the FK
	// check it triggers only re-affirms locks we already hold.
	transfer := domain.Transfer{
		ID:             transferID,
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		Status:         domain.TransferPending,
	}
	if err := s.transfers.Insert(ctx, tx, transfer); err != nil {
		return TransferResult{}, fmt.Errorf("insert transfer: %w", err)
	}

	if fromWallet.Balance < req.Amount {
		return s.failWithTransfer(ctx, tx, transferID, req, domain.ErrInsufficientFunds)
	}

	newFromBalance := fromWallet.Balance - req.Amount
	newToBalance := toWallet.Balance + req.Amount
	if err := s.wallets.UpdateBalance(ctx, tx, req.FromWalletID, newFromBalance); err != nil {
		return TransferResult{}, fmt.Errorf("debit wallet: %w", err)
	}
	if err := s.wallets.UpdateBalance(ctx, tx, req.ToWalletID, newToBalance); err != nil {
		return TransferResult{}, fmt.Errorf("credit wallet: %w", err)
	}

	entries := domain.BuildLedgerEntries(transferID, req.FromWalletID, req.ToWalletID, req.Amount, s.newID(), s.newID())
	if err := s.ledger.InsertEntries(ctx, tx, entries); err != nil {
		return TransferResult{}, fmt.Errorf("insert ledger entries: %w", err)
	}

	if !domain.TransferPending.CanTransitionTo(domain.TransferProcessed) {
		return TransferResult{}, domain.ErrInvalidStateTransition // defensive; unreachable given constants above
	}
	if err := s.transfers.UpdateStatus(ctx, tx, transferID, domain.TransferProcessed, ""); err != nil {
		return TransferResult{}, fmt.Errorf("update transfer status: %w", err)
	}
	transfer.Status = domain.TransferProcessed

	respBody, err := json.Marshal(apiResponse{
		TransferID: transferID, Status: string(domain.TransferProcessed),
		FromWallet: req.FromWalletID, ToWallet: req.ToWalletID, Amount: req.Amount,
	})
	if err != nil {
		return TransferResult{}, fmt.Errorf("marshal response: %w", err)
	}
	if err := s.idem.Complete(ctx, tx, req.IdempotencyKey, transferID, 200, respBody); err != nil {
		return TransferResult{}, fmt.Errorf("complete idempotency record: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return TransferResult{}, fmt.Errorf("commit tx: %w", err)
	}

	s.log.Info("transfer processed",
		"transfer_id", transferID, "from", req.FromWalletID, "to", req.ToWalletID, "amount", req.Amount)

	return TransferResult{Transfer: transfer}, nil
}

// failWithTransfer marks an already-inserted (PENDING) transfer row FAILED,
// stores the idempotent response so retries of this exact request replay
// the same failure, commits, and returns the domain error to the caller.
// Used when the failure is discovered after both wallets were confirmed to
// exist (currently: insufficient funds).
//
// This is still "transactional" in the sense the assignment cares about:
// either the whole failure path commits atomically (transfer=FAILED +
// idempotency record saved) or nothing does, via the single tx.Commit call.
func (s *TransferService) failWithTransfer(ctx context.Context, tx *sql.Tx, transferID string, req domain.NewTransferRequest, cause error) (TransferResult, error) {
	if !domain.TransferPending.CanTransitionTo(domain.TransferFailed) {
		return TransferResult{}, domain.ErrInvalidStateTransition
	}
	if err := s.transfers.UpdateStatus(ctx, tx, transferID, domain.TransferFailed, cause.Error()); err != nil {
		return TransferResult{}, fmt.Errorf("mark transfer failed: %w", err)
	}
	respBody, err := json.Marshal(apiResponse{
		TransferID: transferID, Status: string(domain.TransferFailed),
		FromWallet: req.FromWalletID, ToWallet: req.ToWalletID, Amount: req.Amount,
		Reason: cause.Error(),
	})
	if err != nil {
		return TransferResult{}, fmt.Errorf("marshal failure response: %w", err)
	}
	if err := s.idem.Complete(ctx, tx, req.IdempotencyKey, transferID, 422, respBody); err != nil {
		return TransferResult{}, fmt.Errorf("complete idempotency record (failure path): %w", err)
	}
	if err := tx.Commit(); err != nil {
		return TransferResult{}, fmt.Errorf("commit failure tx: %w", err)
	}
	s.log.Warn("transfer failed", "transfer_id", transferID, "reason", cause.Error())
	return TransferResult{}, cause
}

// failWithoutTransfer handles validation-style failures discovered before a
// transfer row could legally exist (e.g. one of the wallets doesn't exist,
// so a transfers row referencing it would violate its foreign key). Only
// the idempotency record is written, with a null transfer_id, so a retry
// of this exact request replays the same validation failure without a
// dangling/invalid transfer row ever being created.
func (s *TransferService) failWithoutTransfer(ctx context.Context, tx *sql.Tx, req domain.NewTransferRequest, cause error) (TransferResult, error) {
	respBody, err := json.Marshal(apiResponse{
		Status:     string(domain.TransferFailed),
		FromWallet: req.FromWalletID, ToWallet: req.ToWalletID, Amount: req.Amount,
		Reason: cause.Error(),
	})
	if err != nil {
		return TransferResult{}, fmt.Errorf("marshal failure response: %w", err)
	}
	if err := s.idem.Complete(ctx, tx, req.IdempotencyKey, "", 400, respBody); err != nil {
		return TransferResult{}, fmt.Errorf("complete idempotency record (validation failure path): %w", err)
	}
	if err := tx.Commit(); err != nil {
		return TransferResult{}, fmt.Errorf("commit failure tx: %w", err)
	}
	s.log.Warn("transfer rejected before creation", "reason", cause.Error())
	return TransferResult{}, cause
}

// replayError maps a stored failure_reason string back to the original
// domain sentinel error where recognized, so that a replayed failed
// transfer is indistinguishable (from the caller's errors.Is perspective)
// from the original failure. Falls back to a plain error carrying the
// original message for anything unrecognized.
func replayError(reason string) error {
	for _, sentinel := range []error{domain.ErrInsufficientFunds, domain.ErrWalletNotFound} {
		if reason == sentinel.Error() {
			return sentinel
		}
	}
	return errors.New(reason)
}

// fingerprintRequest hashes the semantically meaningful fields of a request
// so we can detect the case where a caller reuses an idempotencyKey for a
// logically different request (a client bug, not a legitimate retry).
func fingerprintRequest(req domain.NewTransferRequest) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%d", req.FromWalletID, req.ToWalletID, req.Amount)
	return hex.EncodeToString(h.Sum(nil))
}

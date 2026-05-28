package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"

	"github.com/PratikDhanave/wallet-transfer-assignment/internal/domain"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/metrics"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/repository"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/tracing"
)

// Pagination bounds applied by ListTransfersByWallet. Public so tests
// and callers (handlers, CLIs) can reference them by name.
const (
	// DefaultTransferListLimit is the page size used when the caller
	// does not request one.
	DefaultTransferListLimit = 50

	// MaxTransferListLimit caps any limit the caller provides. Larger
	// requests are silently clamped (we prefer that over rejecting —
	// a clamp is still a valid, finite response).
	MaxTransferListLimit = 200
)

// CreateTransferRequest is the input to TransferService.Create.
//
// The handler decodes the inbound JSON into this struct and passes it
// in unchanged. The service re-validates the contents because services
// may be invoked from other contexts in future (workers, gRPC, scripts)
// where the handler-level decoding does not apply.
type CreateTransferRequest struct {
	// IdempotencyKey is mandatory. Two requests with the same key
	// must produce the same result; the underlying mechanism is
	// described on CreateTransferRequest.Validate and on the Create
	// method below.
	IdempotencyKey string

	// FromWalletID / ToWalletID are wallet ids. The schema also
	// rejects self-transfers via a CHECK constraint, but we catch it
	// at the service boundary with a clearer error.
	FromWalletID string
	ToWalletID   string

	// Amount is in integer minor units (cents). Money is never
	// float — see AGENTS.md §S-2.
	Amount int64
}

// Validate enforces the inbound contract. It is defence-in-depth: the
// handler has already rejected obvious malformations (DisallowUnknownFields,
// JSON shape) but the service re-checks the semantic rules so that
// non-HTTP callers cannot bypass them.
func (r CreateTransferRequest) Validate() error {
	if r.IdempotencyKey == "" {
		return domain.ErrInvalidIdempotencyKey
	}
	if r.Amount <= 0 {
		return domain.ErrInvalidAmount
	}
	if r.FromWalletID == "" || r.ToWalletID == "" {
		return fmt.Errorf("from/to wallet id is required")
	}
	if r.FromWalletID == r.ToWalletID {
		return domain.ErrSameWallet
	}
	return nil
}

// hash returns a stable digest of the parameters that define the side
// effect of the request.
//
// We hash from|to|amount specifically — not the wire body — because
// JSON ordering and whitespace differences should not count as
// "different request". A replay with the same idempotency key but a
// different (from, to, amount) tuple is rejected with
// ErrIdempotencyConflict; a replay with the same tuple but cosmetically
// different JSON is accepted and replayed normally.
func (r CreateTransferRequest) hash() string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", r.FromWalletID, r.ToWalletID, r.Amount)))
	return hex.EncodeToString(h[:])
}

// TransferService is the only place that opens transactions in the
// codebase. It owns the wallet-locking strategy, the idempotency
// strategy, and the transfer state machine.
type TransferService struct {
	// exec is the *sql.DB used for fast-path reads outside any
	// transaction (specifically the idempotency replay check before
	// we even open a tx).
	exec repository.Executor

	// txm wraps the same *sql.DB and produces transactional
	// Executors for the slow path.
	txm repository.TxManager

	wallets   repository.WalletRepository
	transfers repository.TransferRepository
	ledger    repository.LedgerRepository
	idem      repository.IdempotencyRepository
}

// NewTransferService wires the dependencies. All arguments must be
// non-nil; the constructor does not check.
func NewTransferService(
	exec repository.Executor,
	txm repository.TxManager,
	wallets repository.WalletRepository,
	transfers repository.TransferRepository,
	ledger repository.LedgerRepository,
	idem repository.IdempotencyRepository,
) *TransferService {
	return &TransferService{
		exec:      exec,
		txm:       txm,
		wallets:   wallets,
		transfers: transfers,
		ledger:    ledger,
		idem:      idem,
	}
}

// Create executes a transfer with exactly-once semantics keyed on
// IdempotencyKey.
//
// Concurrency strategy (see AGENTS.md §4):
//   - Each call runs in a database transaction.
//   - Both wallets are locked with SELECT ... FOR UPDATE in a
//     deterministic (lexicographic) order before any balance is read,
//     which prevents both races on the same wallet and deadlocks
//     between transfers that touch the same pair from opposite
//     directions.
//
// Idempotency strategy (see AGENTS.md §5):
//   - We first do a cheap non-transactional read of idempotency_records.
//     If we find the key, we replay the original outcome.
//   - Otherwise we open the tx, insert the side-effect rows, and
//     finally insert the idempotency_records row. The PK on `key`
//     serialises concurrent first-time inserts: the loser receives
//     ErrIdempotencyExists, the whole tx rolls back, and we fall
//     through to the fast-path replay against the now-committed
//     winner row.
//   - Replays with a different request body for the same key return
//     ErrIdempotencyConflict.
//
// Failure semantics:
//   - Insufficient funds is a normal (committed) outcome: the transfer
//     is persisted in state FAILED with a reason, and the idempotency
//     record is committed alongside it. Replays return the same
//     FAILED transfer.
//   - Any other repository error rolls back the tx and surfaces as an
//     error to the handler.
func (s *TransferService) Create(ctx context.Context, req CreateTransferRequest) (*domain.Transfer, error) {
	// Span covers the entire Create call so its duration and any
	// child spans (locks, inserts, commits) appear as one logical
	// unit in trace UIs.
	ctx, span := tracing.Tracer().Start(ctx, "TransferService.Create")
	defer span.End()
	start := time.Now()

	// Validate first; cheapest possible rejection of malformed input.
	if err := req.Validate(); err != nil {
		span.RecordError(err)
		return nil, err
	}
	span.SetAttributes(
		attribute.String("transfer.from_wallet_id", req.FromWalletID),
		attribute.String("transfer.to_wallet_id", req.ToWalletID),
		attribute.Int64("transfer.amount", req.Amount),
	)
	hash := req.hash()

	// Record outcome metrics + span status when we exit. Wrapping
	// this in a deferred closure keeps the branches below readable.
	var resultState string
	defer func() {
		// `resultState` is set below on every terminal path so the
		// counter increments are consistent with the actual state
		// the caller observed.
		if resultState != "" {
			metrics.TransfersTotal.WithLabelValues(resultState).Inc()
			metrics.TransferDuration.WithLabelValues(resultState).Observe(time.Since(start).Seconds())
			span.SetAttributes(attribute.String("transfer.state", resultState))
		}
	}()

	// Fast path: the idempotency record already exists. This is
	// either an honest retry of a request that committed earlier, or
	// a duplicate triggered by client/network behaviour. Reading
	// outside a tx is safe because the winner committed both rows
	// atomically — see Insert path below.
	if t, err := s.replayIfExists(ctx, s.exec, req.IdempotencyKey, hash); err != nil {
		return nil, err
	} else if t != nil {
		// Replays still record metrics; they reflect the original
		// outcome the caller is observing again.
		resultState = string(t.State)
		return t, nil
	}

	// Slow path: open a transaction and try to be the first writer
	// for this key.
	var result *domain.Transfer
	txErr := s.txm.RunInTx(ctx, func(exec repository.Executor) error {
		t, err := s.executeTransfer(ctx, exec, req, hash)
		if err != nil {
			return err
		}
		result = t
		return nil
	})

	// Race-loss path: a concurrent first-time insert beat us to the
	// PK. Postgres only surfaces SQLSTATE 23505 after the blocking
	// conflict transaction commits, so by the time we reach this
	// branch the winning tx has already persisted both the transfer
	// and the idempotency row. The fast-path replay below is therefore
	// guaranteed to find the committed record.
	if errors.Is(txErr, repository.ErrIdempotencyExists) {
		t, err := s.replayIfExists(ctx, s.exec, req.IdempotencyKey, hash)
		if err == nil && t != nil {
			resultState = string(t.State)
		}
		return t, err
	}
	if txErr != nil {
		span.RecordError(txErr)
		return nil, txErr
	}
	if result != nil {
		resultState = string(result.State)
	}
	return result, nil
}

// executeTransfer runs the full create-and-finalise flow inside the
// caller's transaction. It is broken out from Create so the
// race-loss/replay branching stays at one level of indentation.
//
// Steps:
//
//  1. Acquire row-level locks on both wallets in lexicographic id
//     order. This sorting is what prevents the classic A->B / B->A
//     counter-direction deadlock.
//
//  2. Read both wallet balances with the lock held.
//
//  3. Insert the transfer row in PENDING state. (The state column
//     exists so an async-processing variant can be slotted in later;
//     today we drive it to a terminal state in the same tx.)
//
//  4. Insert the idempotency record pointing at the transfer. If this
//     returns ErrIdempotencyExists we bubble it up unchanged; Create
//     handles the replay.
//
//  5. If the source balance cannot cover the amount, drive the
//     transfer to FAILED with a reason and return nil so RunInTx
//     COMMITS the FAILED state (not rollback). Committing the failure
//     is what makes the failure itself idempotent — a retry returns
//     the same FAILED transfer instead of reattempting the debit.
//
//  6. Otherwise: update both balances, write the two ledger entries
//     (DEBIT + CREDIT), and move the transfer to PROCESSED.
func (s *TransferService) executeTransfer(
	ctx context.Context,
	exec repository.Executor,
	req CreateTransferRequest,
	hash string,
) (*domain.Transfer, error) {
	// Step 1: lock both wallets in lex-ascending order so two
	// concurrent transfers in opposite directions cannot deadlock.
	firstID, secondID := orderedPair(req.FromWalletID, req.ToWalletID)
	if _, err := s.wallets.LockForUpdate(ctx, exec, firstID); err != nil {
		return nil, err
	}
	if _, err := s.wallets.LockForUpdate(ctx, exec, secondID); err != nil {
		return nil, err
	}

	// Step 2: read the locked wallets back so we can validate the
	// source balance and compute the new balances.
	src, err := s.wallets.Get(ctx, exec, req.FromWalletID)
	if err != nil {
		return nil, err
	}
	dst, err := s.wallets.Get(ctx, exec, req.ToWalletID)
	if err != nil {
		return nil, err
	}

	// Step 3: insert the transfer in PENDING. The schema CHECK +
	// FK constraints catch any malformed values that slipped past
	// Validate.
	t := &domain.Transfer{
		ID:           uuid.New(),
		FromWalletID: req.FromWalletID,
		ToWalletID:   req.ToWalletID,
		Amount:       req.Amount,
		State:        domain.StatePending,
	}
	if err := s.transfers.Insert(ctx, exec, t); err != nil {
		return nil, err
	}

	// Step 4: claim the idempotency key. ErrIdempotencyExists is the
	// race-loss signal; we bubble it up so Create can replay.
	if err := s.idem.Insert(ctx, exec, req.IdempotencyKey, hash, t.ID); err != nil {
		return nil, err
	}

	// Step 5: insufficient funds is a committed FAILED outcome — see
	// the function doc comment for why we COMMIT rather than rollback.
	if src.Balance < req.Amount {
		const reason = "insufficient funds"
		if err := s.transfers.UpdateState(ctx, exec, t.ID, domain.StateFailed, reason); err != nil {
			return nil, err
		}
		t.State = domain.StateFailed
		t.FailureReason = reason
		return t, nil
	}

	// Step 6: happy path. Mutate the balances and write the
	// double-entry ledger rows. Order doesn't matter functionally
	// (we hold locks on both wallets) but DEBIT-then-CREDIT is the
	// conventional reading order in audit reports.
	if err := s.wallets.UpdateBalance(ctx, exec, src.ID, src.Balance-req.Amount); err != nil {
		return nil, err
	}
	if err := s.wallets.UpdateBalance(ctx, exec, dst.ID, dst.Balance+req.Amount); err != nil {
		return nil, err
	}
	if err := s.ledger.Insert(ctx, exec, &domain.LedgerEntry{
		TransferID: t.ID,
		WalletID:   src.ID,
		Type:       domain.EntryDebit,
		Amount:     req.Amount,
	}); err != nil {
		return nil, err
	}
	if err := s.ledger.Insert(ctx, exec, &domain.LedgerEntry{
		TransferID: t.ID,
		WalletID:   dst.ID,
		Type:       domain.EntryCredit,
		Amount:     req.Amount,
	}); err != nil {
		return nil, err
	}

	// Finalise: drive the state machine to PROCESSED. The schema
	// CHECK and the in-memory CanTransitionTo (domain package) both
	// guard against invalid moves.
	if err := s.transfers.UpdateState(ctx, exec, t.ID, domain.StateProcessed, ""); err != nil {
		return nil, err
	}
	t.State = domain.StateProcessed
	return t, nil
}

// replayIfExists implements the idempotent-replay path.
//
// It looks up the idempotency record. If absent, returns (nil, nil)
// — the caller's contract is "nil transfer + nil error means proceed
// to the slow path". If present, the stored request_hash is compared
// against the caller's hash; a mismatch returns ErrIdempotencyConflict
// (the handler maps this to 409). Otherwise the original transfer is
// fetched and returned unchanged.
func (s *TransferService) replayIfExists(
	ctx context.Context,
	exec repository.Executor,
	key, hash string,
) (*domain.Transfer, error) {
	rec, err := s.idem.Get(ctx, exec, key)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if rec.RequestHash != hash {
		return nil, domain.ErrIdempotencyConflict
	}
	return s.transfers.Get(ctx, exec, rec.TransferID)
}

// GetTransfer returns a transfer by id. Surfaces
// repository.ErrNotFound when the row does not exist; the handler
// maps that to HTTP 404.
func (s *TransferService) GetTransfer(ctx context.Context, id uuid.UUID) (*domain.Transfer, error) {
	return s.transfers.Get(ctx, s.exec, id)
}

// ListTransfersByWallet returns the transfer history for a wallet,
// most-recent-first, with cursor-based pagination.
//
// `limit` is clamped to [1, MaxTransferListLimit]. A zero or
// negative value falls back to DefaultTransferListLimit.
// `before` is the createdAt cursor from the previous page (pass nil
// for the first page).
//
// The function returns the transfers plus an optional `next` cursor:
// non-nil when the result is full (limit reached) and a subsequent
// page may exist. Callers should keep paginating until next is nil.
func (s *TransferService) ListTransfersByWallet(
	ctx context.Context,
	walletID string,
	limit int,
	before *time.Time,
) (transfers []domain.Transfer, next *time.Time, err error) {
	if walletID == "" {
		return nil, nil, domain.ErrWalletNotFound
	}
	if limit <= 0 {
		limit = DefaultTransferListLimit
	}
	if limit > MaxTransferListLimit {
		limit = MaxTransferListLimit
	}

	rows, err := s.transfers.ListByWallet(ctx, s.exec, walletID, limit, before)
	if err != nil {
		return nil, nil, err
	}

	// Compute the next cursor only if the page is full — otherwise
	// the caller has reached the end and there's nothing past it.
	if len(rows) == limit {
		last := rows[len(rows)-1].CreatedAt
		next = &last
	}
	return rows, next, nil
}

// orderedPair returns its two arguments in lexicographic ascending
// order. The whole point is the deterministic wallet lock order
// described on executeTransfer — see AGENTS.md §S-4.
func orderedPair(a, b string) (string, string) {
	if a < b {
		return a, b
	}
	return b, a
}

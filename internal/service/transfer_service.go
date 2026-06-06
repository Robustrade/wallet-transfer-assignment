package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
)

// idempotency retry configuration.
const (
	// idemMaxRetries is the maximum number of times Execute will poll the
	// idempotency_records table after losing the INSERT race to another goroutine.
	idemMaxRetries = 10
	// idemRetryDelay is the backoff pause between idempotency record polls.
	idemRetryDelay = 50 * time.Millisecond
	// MaxIdempotencyKeyLength is the maximum allowed length for an idempotency key.
	// Enforced in the handler before reaching the service.
	MaxIdempotencyKeyLength = 255

	// magicMinAmount is the minimum random deposit amount in cents (inclusive).
	magicMinAmount domain.Amount = 100
	// magicMaxAmount is the maximum random deposit amount in cents (inclusive).
	magicMaxAmount domain.Amount = 10_000
)

// MagicRequest holds the input for a magic (random nature) deposit operation.
type MagicRequest struct {
	IdempotencyKey string
	ToWalletID     uuid.UUID
}

// TransferRequest holds the input for a transfer operation.
type TransferRequest struct {
	IdempotencyKey string
	FromWalletID   uuid.UUID
	ToWalletID     uuid.UUID
	Amount         domain.Amount
}

// TransferResponse is the result returned to the caller and cached for
// idempotency replay.
type TransferResponse struct {
	TransferID uuid.UUID             `json:"id"`
	Status     domain.TransferStatus `json:"status"`
	Amount     domain.Amount         `json:"amount"`
	FromCache  bool                  `json:"-"` // true when served from idempotency cache
}

// TransferService orchestrates the wallet transfer workflow.
type TransferService struct {
	db        DB
	wallets   repository.WalletRepository
	transfers repository.TransferRepository
	ledger    repository.LedgerRepository
	idem      repository.IdempotencyRepository
	log       *slog.Logger
}

// NewTransferService constructs a TransferService with the supplied
// dependencies. All parameters are required.
func NewTransferService(
	db DB,
	wallets repository.WalletRepository,
	transfers repository.TransferRepository,
	ledger repository.LedgerRepository,
	idem repository.IdempotencyRepository,
	log *slog.Logger,
) *TransferService {
	return &TransferService{
		db:        db,
		wallets:   wallets,
		transfers: transfers,
		ledger:    ledger,
		idem:      idem,
		log:       log,
	}
}

// Execute performs a wallet-to-wallet transfer or returns the cached result
// if the idempotency key was already used.
//
// Workflow:
//  1. Validate inputs (same wallet, non-positive amount).
//  2. Look up idempotency record → return early if found.
//  3. BEGIN transaction.
//  4. Lock wallets FOR UPDATE in deterministic UUID order (prevents deadlocks).
//  5. Validate source balance.
//  6. Create transfer record (PENDING).
//  7. Update wallet balances.
//  8. Insert two ledger entries (DEBIT + CREDIT).
//  9. Update transfer status → PROCESSED.
//  10. Write idempotency record.
//  11. COMMIT.
func (s *TransferService) Execute(ctx context.Context, req TransferRequest) (TransferResponse, error) {
	if err := s.validate(req); err != nil {
		return TransferResponse{}, err
	}

	// ── 1. Idempotency fast-path ────────────────────────────────────────────
	if cached, err := s.idem.Get(ctx, s.db, req.IdempotencyKey); err == nil {
		// Detect key reuse with different parameters. An empty hash means the
		// record predates conflict detection (treat as a valid match).
		if cached.RequestHash != "" && cached.RequestHash != computeRequestHash(req) {
			return TransferResponse{}, domain.ErrDuplicateIdempotencyKey
		}

		s.log.Info("idempotency cache hit",
			"layer", "svc",
			"idempotencyKey", req.IdempotencyKey,
			"transferID", cached.TransferID,
		)

		var resp TransferResponse
		jsonErr := json.Unmarshal([]byte(cached.ResponseJSON), &resp)
		if jsonErr == nil {
			resp.FromCache = true

			return resp, nil
		}

		// If unmarshaling fails, log the error and continue to attempt processing the transfer as normal.
		// The cache record will be overwritten on success, so this is a self-healing scenario.
		s.log.Error("failed to unmarshal idempotency cache record",
			"layer", "svc",
			"idempotencyKey", req.IdempotencyKey,
			"error", jsonErr,
		)
	}

	// ── 2. Transactional transfer ───────────────────────────────────────────
	// Use READ COMMITTED isolation with SELECT FOR UPDATE for safe balance updates.
	// READ COMMITTED is the PostgreSQL default but we set it explicitly for clarity.
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return TransferResponse{}, fmt.Errorf("svc: begin transaction: %w", err)
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	// Lock wallets in ascending UUID order to prevent deadlocks.
	fromW, toW, lockErr := s.lockWallets(ctx, tx, req.FromWalletID, req.ToWalletID)
	if lockErr != nil {
		err = lockErr

		return TransferResponse{}, err
	}

	// Balance check.
	if fromW.Balance < req.Amount {
		err = domain.ErrInsufficientFunds
		s.log.Warn("insufficient funds",
			"layer", "svc",
			"idempotencyKey", req.IdempotencyKey,
			"walletID", fromW.ID,
			"balance", fromW.Balance,
			"requested", req.Amount,
		)

		return TransferResponse{}, err
	}

	// Create transfer in PENDING state.
	transfer := domain.NewTransfer(req.FromWalletID, req.ToWalletID, req.IdempotencyKey, req.Amount)

	if createErr := s.transfers.Create(ctx, tx, transfer); createErr != nil {
		// A duplicate idempotency key means another goroutine won the race and
		// already created this transfer. Roll back, then wait for that goroutine
		// to commit and return its cached response.
		if isUniqueViolation(createErr) {
			_ = tx.Rollback(ctx)
			err = nil // clear so the deferred rollback is a no-op

			return s.waitForIdempotencyRecord(ctx, req)
		}

		err = fmt.Errorf("svc: create transfer: %w", createErr)

		return TransferResponse{}, err
	}

	s.log.Info("transfer state transition",
		"layer", "svc",
		"idempotencyKey", req.IdempotencyKey,
		"transferID", transfer.ID,
		"from", domain.TransferStatusPending.String(),
		"to", "executing",
	)

	// Update balances.
	if updateErr := s.wallets.UpdateBalance(ctx, tx, fromW.ID, fromW.Balance-req.Amount); updateErr != nil {
		err = fmt.Errorf("svc: debit wallet: %w", updateErr)

		return TransferResponse{}, err
	}

	if updateErr := s.wallets.UpdateBalance(ctx, tx, toW.ID, toW.Balance+req.Amount); updateErr != nil {
		err = fmt.Errorf("svc: credit wallet: %w", updateErr)

		return TransferResponse{}, err
	}

	// Insert double-entry ledger records.
	debit := domain.NewLedgerEntry(transfer.ID, fromW.ID, domain.EntryTypeDebit, req.Amount)
	credit := domain.NewLedgerEntry(transfer.ID, toW.ID, domain.EntryTypeCredit, req.Amount)

	if ledgerErr := s.ledger.CreateEntries(ctx, tx, debit, credit); ledgerErr != nil {
		err = fmt.Errorf("svc: create ledger entries: %w", ledgerErr)

		return TransferResponse{}, err
	}

	// Enforce the domain state machine before persisting the status change.
	if !domain.CanTransition(transfer.Status, domain.TransferStatusProcessed) {
		err = fmt.Errorf("svc: %w: %s → %s",
			domain.ErrInvalidTransition, transfer.Status, domain.TransferStatusProcessed)

		return TransferResponse{}, err
	}

	// Transition transfer to PROCESSED.
	if statusErr := s.transfers.UpdateStatus(ctx, tx, transfer.ID, domain.TransferStatusProcessed); statusErr != nil {
		err = fmt.Errorf("svc: update transfer status: %w", statusErr)

		return TransferResponse{}, err
	}

	s.log.Info("transfer state transition",
		"layer", "svc",
		"idempotencyKey", req.IdempotencyKey,
		"transferID", transfer.ID,
		"fromWalletID", fromW.ID,
		"toWalletID", toW.ID,
		"amount", req.Amount,
		"from", domain.TransferStatusPending.String(),
		"to", domain.TransferStatusProcessed.String(),
	)

	// Build and cache the response JSON atomically within the transaction.
	resp := TransferResponse{
		TransferID: transfer.ID,
		Status:     domain.TransferStatusProcessed,
		Amount:     req.Amount,
	}

	respJSON, marshalErr := json.Marshal(resp)
	if marshalErr != nil {
		err = fmt.Errorf("svc: marshal response: %w", marshalErr)

		return TransferResponse{}, err
	}

	idemRecord := repository.IdempotencyRecord{
		Key:          req.IdempotencyKey,
		TransferID:   transfer.ID,
		ResponseJSON: string(respJSON),
		StatusCode:   200,
		RequestHash:  computeRequestHash(req),
	}

	if idemErr := s.idem.Create(ctx, tx, idemRecord); idemErr != nil {
		err = fmt.Errorf("svc: store idempotency record: %w", idemErr)

		return TransferResponse{}, err
	}

	if commitErr := tx.Commit(ctx); commitErr != nil {
		err = fmt.Errorf("svc: commit transaction: %w", commitErr)

		return TransferResponse{}, err
	}

	return resp, nil
}

// GetTransfer retrieves a transfer by ID.
func (s *TransferService) GetTransfer(ctx context.Context, id uuid.UUID) (domain.Transfer, error) {
	t, err := s.transfers.GetByID(ctx, s.db, id)
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("svc: get transfer: %w", err)
	}

	return t, nil
}

// GetWallet retrieves a wallet by ID.
func (s *TransferService) GetWallet(ctx context.Context, id uuid.UUID) (domain.Wallet, error) {
	w, err := s.wallets.GetByID(ctx, s.db, id)
	if err != nil {
		return domain.Wallet{}, fmt.Errorf("svc: get wallet: %w", err)
	}

	return w, nil
}

// CreateWallet creates a new wallet with a zero balance and a freshly generated
// UUID v7, persists it, and returns the created wallet.
func (s *TransferService) CreateWallet(ctx context.Context) (domain.Wallet, error) {
	w := domain.NewWallet()

	if err := s.wallets.Create(ctx, s.db, w); err != nil {
		return domain.Wallet{}, fmt.Errorf("svc: create wallet: %w", err)
	}

	s.log.Info("wallet created", "layer", "svc", "walletID", w.ID)

	return w, nil
}

// Magic executes a deposit from the nature wallet to the given destination
// wallet for a randomly chosen amount in the range [100, 10 000] cents.
//
// The idempotency cache is consulted before the random amount is sampled so
// that repeated calls with the same key always replay the exact amount that
// was committed on the first call. In the rare concurrent race where two
// goroutines both miss the pre-check and generate different amounts, Magic
// reads the winning goroutine's committed record from the cache.
func (s *TransferService) Magic(ctx context.Context, req MagicRequest) (TransferResponse, error) {
	// Fast-path: return the cached response without generating a new amount.
	if cached, err := s.idem.Get(ctx, s.db, req.IdempotencyKey); err == nil {
		var resp TransferResponse
		if jsonErr := json.Unmarshal([]byte(cached.ResponseJSON), &resp); jsonErr == nil {
			resp.FromCache = true
			s.log.Info("magic idempotency cache hit",
				"layer", "svc",
				"idempotencyKey", req.IdempotencyKey,
				"transferID", cached.TransferID,
			)

			return resp, nil
		}
	}

	amount := magicMinAmount + domain.Amount(rand.Int63n(int64(magicMaxAmount-magicMinAmount+1)))

	resp, err := s.Execute(ctx, TransferRequest{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   domain.NatureWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         amount,
	})

	// Concurrent race: another goroutine won the INSERT with a different random
	// amount, causing a hash mismatch inside Execute. Read the committed record.
	if errors.Is(err, domain.ErrDuplicateIdempotencyKey) {
		return s.waitForMagicRecord(ctx, req.IdempotencyKey)
	}

	return resp, err
}

// waitForMagicRecord polls the idempotency cache until the record committed by
// the winning concurrent goroutine becomes visible, then returns it.
func (s *TransferService) waitForMagicRecord(ctx context.Context, key string) (TransferResponse, error) {
	for i := range idemMaxRetries {
		if i > 0 {
			select {
			case <-ctx.Done():
				return TransferResponse{}, ctx.Err()
			case <-time.After(idemRetryDelay):
			}
		}

		if cached, err := s.idem.Get(ctx, s.db, key); err == nil {
			var resp TransferResponse
			if jsonErr := json.Unmarshal([]byte(cached.ResponseJSON), &resp); jsonErr == nil {
				resp.FromCache = true

				return resp, nil
			}
		}
	}

	return TransferResponse{}, fmt.Errorf("svc: magic idempotency record not visible after race: key=%s", key)
}

// validate performs cheap, stateless input validation.
func (s *TransferService) validate(req TransferRequest) error {
	if req.Amount <= 0 {
		return domain.ErrInvalidAmount
	}

	if req.FromWalletID == req.ToWalletID {
		return domain.ErrSameWallet
	}

	return nil
}

// lockWallets acquires FOR UPDATE locks on both wallets in ascending UUID byte
// order to prevent deadlocks when two concurrent transfers target the same pair.
func (s *TransferService) lockWallets(
	ctx context.Context,
	tx pgx.Tx,
	fromID, toID uuid.UUID,
) (fromW, toW domain.Wallet, err error) {
	first, second := fromID, toID
	if bytes.Compare(toID[:], fromID[:]) < 0 {
		first, second = toID, fromID
	}

	w1, err := s.wallets.GetByIDForUpdate(ctx, tx, first)
	if err != nil {
		s.log.Error("failed to lock wallet", "layer", "svc", "walletID", first, "error", err)

		return domain.Wallet{}, domain.Wallet{}, fmt.Errorf("svc: lock wallet: %w", err)
	}

	w2, err := s.wallets.GetByIDForUpdate(ctx, tx, second)
	if err != nil {
		s.log.Error("failed to lock wallet", "layer", "svc", "walletID", second, "error", err)

		return domain.Wallet{}, domain.Wallet{}, fmt.Errorf("svc: lock wallet: %w", err)
	}

	// Re-assign so fromW and toW match the caller's perspective.
	if first == fromID {
		return w1, w2, nil
	}

	return w2, w1, nil
}

// waitForIdempotencyRecord retries the idempotency lookup with backoff to
// handle the race where another goroutine won the INSERT but has not committed
// yet. Returns as soon as the record is visible or after max retries.
// It also checks for request-parameter conflicts.
func (s *TransferService) waitForIdempotencyRecord(ctx context.Context, req TransferRequest) (TransferResponse, error) {
	reqHash := computeRequestHash(req)

	for i := range idemMaxRetries {
		if i > 0 {
			select {
			case <-ctx.Done():
				return TransferResponse{}, ctx.Err()
			case <-time.After(idemRetryDelay):
			}
		}

		cached, err := s.idem.Get(ctx, s.db, req.IdempotencyKey)
		if err == nil {
			// Detect key reuse with different parameters.
			if cached.RequestHash != "" && cached.RequestHash != reqHash {
				return TransferResponse{}, domain.ErrDuplicateIdempotencyKey
			}

			s.log.Info("idempotency cache hit after race",
				"layer", "svc",
				"idempotencyKey", req.IdempotencyKey,
				"transferID", cached.TransferID,
				"attempt", i+1,
			)

			var resp TransferResponse
			if jsonErr := json.Unmarshal([]byte(cached.ResponseJSON), &resp); jsonErr != nil {
				return TransferResponse{}, fmt.Errorf("svc: unmarshal cached response: %w", jsonErr)
			}

			resp.FromCache = true

			return resp, nil
		}
	}

	return TransferResponse{}, fmt.Errorf("svc: idempotency record not visible after race: key=%s", req.IdempotencyKey)
}

// isUniqueViolation checks whether err is a PostgreSQL unique constraint
// violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// computeRequestHash returns a SHA-256 hex digest of the canonical transfer
// request parameters (fromWalletID, toWalletID, amount). Stored alongside the
// idempotency record so that key reuse with different parameters can be
// detected and rejected.
func computeRequestHash(req TransferRequest) string {
	h := sha256.New()
	h.Write([]byte(req.FromWalletID.String()))
	h.Write([]byte("|"))
	h.Write([]byte(req.ToWalletID.String()))
	h.Write([]byte("|"))
	h.Write([]byte(strconv.FormatInt(int64(req.Amount), 10)))

	return hex.EncodeToString(h.Sum(nil))
}

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/rajdeep-multipl/wallet-transfer-assignment/internal/models"
)

// ---------------------------------------------------------------------------
// Dependencies (interfaces live here so unit tests can supply fakes)
// ---------------------------------------------------------------------------

// Transactor hands out database handles. DB is the plain auto-commit handle;
// WithTx runs fn inside a transaction and commits if fn returns nil, rolls
// back if it returns an error (or panics).
type Transactor interface {
	DB(ctx context.Context) *gorm.DB
	WithTx(ctx context.Context, fn func(tx *gorm.DB) error) error
}

type WalletRepository interface {
	LockPair(tx *gorm.DB, fromID, toID uuid.UUID) (from, to *models.Wallet, err error)
	Debit(tx *gorm.DB, id uuid.UUID, amount decimal.Decimal) error
	Credit(tx *gorm.DB, id uuid.UUID, amount decimal.Decimal) error
}

type TransferRepository interface {
	Insert(tx *gorm.DB, t *models.Transfer) error
}

type LedgerRepository interface {
	InsertPair(tx *gorm.DB, debit, credit *models.LedgerEntry) error
}

type IdempotencyRepository interface {
	Claim(db *gorm.DB, key, requestHash string) (bool, error)
	Get(db *gorm.DB, key string) (rec *models.IdempotencyRecord, found bool, err error)
	ReclaimStale(db *gorm.DB, key string, timeout time.Duration) (bool, error)
	Finalize(db *gorm.DB, key string, status models.IdempotencyStatus, transferID *uuid.UUID, body []byte) (bool, error)
}

// ---------------------------------------------------------------------------
// Service
// ---------------------------------------------------------------------------

const defaultStaleAfter = 30 * time.Second

// Config holds tunables. StaleAfter is how long an IN_PROGRESS idempotency row
// may go untouched before a retry is allowed to take it over. Keep it well
// above the slowest healthy transfer.
type Config struct {
	StaleAfter time.Duration
}

type TransferService struct {
	tx         Transactor
	wallets    WalletRepository
	transfers  TransferRepository
	ledger     LedgerRepository
	idem       IdempotencyRepository
	staleAfter time.Duration
}

func NewTransferService(
	tx Transactor,
	wallets WalletRepository,
	transfers TransferRepository,
	ledger LedgerRepository,
	idem IdempotencyRepository,
	cfg Config,
) *TransferService {
	if cfg.StaleAfter <= 0 {
		cfg.StaleAfter = defaultStaleAfter
	}
	return &TransferService{
		tx:         tx,
		wallets:    wallets,
		transfers:  transfers,
		ledger:     ledger,
		idem:       idem,
		staleAfter: cfg.StaleAfter,
	}
}

// TransferInput is one transfer request. The caller (handler) is responsible
// for rejecting an empty IdempotencyKey.
type TransferInput struct {
	IdempotencyKey string
	FromWalletID   uuid.UUID
	ToWalletID     uuid.UUID
	Amount         decimal.Decimal
}

// TransferResult describes a successful transfer. Replayed is true when this
// is the stored result of an earlier identical request, not a new transfer.
type TransferResult struct {
	TransferID uuid.UUID
	Status     models.TransferStatus
	Replayed   bool
}

// errClaimLost means another attempt already finalized this idempotency key
// while this attempt was running. The transaction is rolled back so the
// transfer is not applied twice, and the stored result is replayed instead.
var errClaimLost = errors.New("idempotency claim lost to another attempt")

// CreateTransfer processes a transfer exactly once per idempotency key.
//
// Failures return the sentinel errors from the models package (ErrWalletNotFound,
// ErrInsufficientFunds, ErrKeyMismatch, ErrInProgress, ...); anything else is
// an unexpected error the handler should treat as a 500.
func (s *TransferService) CreateTransfer(ctx context.Context, in TransferInput) (*TransferResult, error) {
	// Stateless input checks run before the claim: they fail identically every
	// time, so they are naturally idempotent and need no stored record. They
	// must also run before hashing (see computeRequestHash).
	if err := validateInput(in); err != nil {
		return nil, err
	}

	requestHash := computeRequestHash(in.FromWalletID, in.ToWalletID, in.Amount)
	db := s.tx.DB(ctx)

	// Claim commits on its own (plain db, not the transfer transaction) so the
	// record survives a rollback of the transfer.
	claimed, err := s.idem.Claim(db, in.IdempotencyKey, requestHash)
	if err != nil {
		return nil, fmt.Errorf("claim idempotency key: %w", err)
	}
	if !claimed {
		res, proceed, err := s.resolveExistingKey(db, in.IdempotencyKey, requestHash)
		if !proceed {
			return res, err
		}
		// proceed == true: we reclaimed a stale row and now own the request.
	}

	return s.execute(ctx, db, in)
}

// resolveExistingKey handles a key that already has a row. It returns
// proceed=true only when the caller has taken over a stale IN_PROGRESS row
// and must now run the transfer.
func (s *TransferService) resolveExistingKey(db *gorm.DB, key, requestHash string) (res *TransferResult, proceed bool, err error) {
	rec, found, err := s.idem.Get(db, key)
	if err != nil {
		return nil, false, fmt.Errorf("load idempotency record: %w", err)
	}
	if !found {
		// Rows are never deleted, so this should not happen. Treat as retryable.
		return nil, false, models.ErrInProgress
	}

	// The hash check gates everything: a different payload under the same key
	// is a client bug, whatever the row's status.
	if rec.RequestHash != requestHash {
		return nil, false, models.ErrKeyMismatch
	}

	if rec.Status != models.IdempotencyInProgress {
		res, err := replay(rec)
		return res, false, err
	}

	won, err := s.idem.ReclaimStale(db, key, s.staleAfter)
	if err != nil {
		return nil, false, fmt.Errorf("reclaim idempotency key: %w", err)
	}
	if won {
		return nil, true, nil
	}

	// Either the row is still fresh (someone is working on it) or another
	// retry won the reclaim. It may also have finished in the meantime.
	res, err = s.replayIfFinished(db, key)
	return res, false, err
}

// replayIfFinished replays the stored result if the row has reached a final
// state, and otherwise reports that the request is still in progress.
func (s *TransferService) replayIfFinished(db *gorm.DB, key string) (*TransferResult, error) {
	rec, found, err := s.idem.Get(db, key)
	if err != nil {
		return nil, fmt.Errorf("reload idempotency record: %w", err)
	}
	if !found || rec.Status == models.IdempotencyInProgress {
		return nil, models.ErrInProgress
	}
	return replay(rec)
}

// outcome carries the result out of the transaction callback.
type outcome struct {
	result *TransferResult // set when the transfer succeeded
	bizErr error           // set when a FAILED outcome was committed (insufficient funds)
}

func (s *TransferService) execute(ctx context.Context, db *gorm.DB, in TransferInput) (*TransferResult, error) {
	var out outcome
	err := withDeadlockRetry(func() error {
		out = outcome{} // reset in case this is the retry
		return s.tx.WithTx(ctx, func(tx *gorm.DB) error {
			return s.transferInTx(tx, in, &out)
		})
	})

	switch {
	case err == nil && out.bizErr != nil:
		// FAILED outcome was committed inside the transaction.
		return nil, out.bizErr
	case err == nil:
		return out.result, nil
	case errors.Is(err, errClaimLost):
		return s.replayIfFinished(db, in.IdempotencyKey)
	case codeFor(err) != "":
		// Business failure that rolled back (wallet not found). Nothing moved,
		// so record the failure on the plain handle, outside the rolled-back tx.
		if ferr := s.finalizeFailed(db, in.IdempotencyKey, err); ferr != nil {
			return nil, ferr
		}
		return nil, err
	default:
		// Unexpected (DB down, constraint violation, ...). The idempotency row
		// stays IN_PROGRESS and becomes reclaimable after StaleAfter.
		return nil, fmt.Errorf("execute transfer: %w", err)
	}
}

// transferInTx is the atomic unit: everything here commits or rolls back
// together. Returning an error rolls back.
func (s *TransferService) transferInTx(tx *gorm.DB, in TransferInput, out *outcome) error {
	// Lock both wallets, in fixed id order (done in SQL).
	from, to, err := s.wallets.LockPair(tx, in.FromWalletID, in.ToWalletID)
	if err != nil {
		return err
	}

	// The balance is read under the row lock, so nobody can change it between
	// this check and the debit below.
	if from.Balance.LessThan(in.Amount) {
		// Nothing has been modified yet, so commit a FAILED outcome instead of
		// rolling back: keep a FAILED transfer row (no ledger rows) and cache
		// the failure for replay.
		t := &models.Transfer{
			FromWalletID: from.ID,
			ToWalletID:   to.ID,
			Amount:       in.Amount,
			Status:       models.TransferFailed,
		}
		if err := s.transfers.Insert(tx, t); err != nil {
			return fmt.Errorf("insert failed transfer: %w", err)
		}
		body, err := failureBody(models.ErrInsufficientFunds, &t.ID)
		if err != nil {
			return err
		}
		if err := s.finalizeInTx(tx, in.IdempotencyKey, models.IdempotencyFailed, t.ID, body); err != nil {
			return err
		}
		out.bizErr = models.ErrInsufficientFunds
		return nil
	}

	t := &models.Transfer{
		FromWalletID: from.ID,
		ToWalletID:   to.ID,
		Amount:       in.Amount,
		Status:       models.TransferProcessed,
	}
	if err := s.transfers.Insert(tx, t); err != nil {
		return fmt.Errorf("insert transfer: %w", err)
	}

	if err := s.wallets.Debit(tx, from.ID, in.Amount); err != nil {
		return fmt.Errorf("debit sender: %w", err)
	}
	if err := s.wallets.Credit(tx, to.ID, in.Amount); err != nil {
		return fmt.Errorf("credit receiver: %w", err)
	}

	debit := &models.LedgerEntry{TransferID: t.ID, WalletID: from.ID, Type: models.EntryDebit, Amount: in.Amount}
	credit := &models.LedgerEntry{TransferID: t.ID, WalletID: to.ID, Type: models.EntryCredit, Amount: in.Amount}
	if err := s.ledger.InsertPair(tx, debit, credit); err != nil {
		return fmt.Errorf("insert ledger entries: %w", err)
	}

	// Last step, inside the same transaction: the money movement and COMPLETED
	// commit together. If another attempt already finalized this key, roll back.
	body, err := successBody(t.ID)
	if err != nil {
		return err
	}
	if err := s.finalizeInTx(tx, in.IdempotencyKey, models.IdempotencyCompleted, t.ID, body); err != nil {
		return err
	}

	out.result = &TransferResult{TransferID: t.ID, Status: models.TransferProcessed}
	return nil
}

func (s *TransferService) finalizeInTx(tx *gorm.DB, key string, status models.IdempotencyStatus, transferID uuid.UUID, body []byte) error {
	applied, err := s.idem.Finalize(tx, key, status, &transferID, body)
	if err != nil {
		return fmt.Errorf("finalize idempotency record: %w", err)
	}
	if !applied {
		return errClaimLost
	}
	return nil
}

// finalizeFailed records a business failure that has no transfer row.
func (s *TransferService) finalizeFailed(db *gorm.DB, key string, bizErr error) error {
	body, err := failureBody(bizErr, nil)
	if err != nil {
		return err
	}
	// applied == false means another attempt already finalized it; the stored
	// result is equivalent, so it is fine to ignore.
	if _, err := s.idem.Finalize(db, key, models.IdempotencyFailed, nil, body); err != nil {
		return fmt.Errorf("finalize failed request: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

var maxAmount = decimal.RequireFromString("10000000000000") // 10 trillion

func validateInput(in TransferInput) error {
	if in.FromWalletID == in.ToWalletID {
		return models.ErrSameWallet
	}
	a := in.Amount
	if !a.IsPositive() || a.GreaterThan(maxAmount) {
		return models.ErrInvalidAmount
	}
	// More than two decimal places would be silently rounded by StringFixed(2)
	// in the hash and by NUMERIC(16,2) in the database. Compare by value, not by
	// Exponent(): "100.500" has exponent -3 but is a valid two-place amount.
	if !a.Round(2).Equal(a) {
		return models.ErrInvalidAmount
	}
	return nil
}

// ---------------------------------------------------------------------------
// Cached response envelope (stored in idempotency_records.response_body)
// ---------------------------------------------------------------------------

type responseEnvelope struct {
	Status     string `json:"status"` // PROCESSED | FAILED
	TransferID string `json:"transfer_id,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"`
}

// Only failures that are cached appear here.
var errorCodes = []struct {
	err  error
	code string
}{
	{models.ErrInsufficientFunds, "INSUFFICIENT_FUNDS"},
	{models.ErrWalletNotFound, "WALLET_NOT_FOUND"},
}

func codeFor(err error) string {
	for _, e := range errorCodes {
		if errors.Is(err, e.err) {
			return e.code
		}
	}
	return ""
}

func errFromCode(code string) error {
	for _, e := range errorCodes {
		if e.code == code {
			return e.err
		}
	}
	return fmt.Errorf("unknown cached error code %q", code)
}

func successBody(transferID uuid.UUID) ([]byte, error) {
	return json.Marshal(responseEnvelope{
		Status:     string(models.TransferProcessed),
		TransferID: transferID.String(),
	})
}

func failureBody(err error, transferID *uuid.UUID) ([]byte, error) {
	env := responseEnvelope{
		Status:    string(models.TransferFailed),
		ErrorCode: codeFor(err),
	}
	if transferID != nil {
		env.TransferID = transferID.String()
	}
	return json.Marshal(env)
}

// replay turns a stored result back into what the original request returned.
func replay(rec *models.IdempotencyRecord) (*TransferResult, error) {
	var env responseEnvelope
	if err := json.Unmarshal(rec.ResponseBody, &env); err != nil {
		return nil, fmt.Errorf("decode cached response: %w", err)
	}
	if env.Status == string(models.TransferProcessed) {
		id, err := uuid.Parse(env.TransferID)
		if err != nil {
			return nil, fmt.Errorf("decode cached transfer id: %w", err)
		}
		return &TransferResult{TransferID: id, Status: models.TransferProcessed, Replayed: true}, nil
	}
	return nil, errFromCode(env.ErrorCode)
}

// ---------------------------------------------------------------------------
// Deadlock retry
// ---------------------------------------------------------------------------

// withDeadlockRetry runs fn and, if Postgres aborted it as a deadlock victim
// (SQLSTATE 40P01), runs it once more. Fixed lock ordering makes deadlocks
// structurally unlikely; this is the backstop from ADR section 10.
func withDeadlockRetry(fn func() error) error {
	err := fn()
	if isDeadlock(err) {
		err = fn()
	}
	return err
}

func isDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40P01"
}

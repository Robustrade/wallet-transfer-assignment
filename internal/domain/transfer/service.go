package transfer

import (
	"context"
	"errors"
	"fmt"

	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/domain/ledger"
	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/domain/wallet"
	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/platform/timeutil"
)

// ErrDuplicateTxn is returned when a txnId already maps to an existing transfer.
// The caller should treat this as a successful idempotent response, not an error.
var ErrDuplicateTxn = errors.New("transfer: duplicate txnId")

// CreateRequest is the input to the transfer execution workflow.
type CreateRequest struct {
	TxnID        string
	FromWalletID string
	ToWalletID   string
	Amount       int64
}

// Service orchestrates the full transfer workflow and owns the transfer state machine.
//
// Cross-domain dependencies:
//   - wallet.Repository — validates wallet existence + active status (one SQL query)
//   - ledger.Service    — CommitToLedger owns its own transaction; no tx passes across domains
type Service struct {
	repo       Repository
	walletRepo wallet.Repository
	ledgerSvc  *ledger.Service
}

// NewService constructs a TransferService with all required dependencies.
func NewService(repo Repository, walletRepo wallet.Repository, ledgerSvc *ledger.Service) *Service {
	return &Service{
		repo:       repo,
		walletRepo: walletRepo,
		ledgerSvc:  ledgerSvc,
	}
}

// Execute runs the full transfer workflow.
//
// Workflow:
//  1. Idempotency check   — if txnId seen in PENDING state, resume workflow from DB data.
//     If already in a terminal state (PROCESSED/FAILED), return immediately.
//     If no record exists, persist a new PENDING record.
//  2. Validate transfer   — amount, wallet IDs; business failure marks transfer FAILED.
//  3. Validate wallets    — single SQL query (existence + ACTIVE); business failure marks FAILED.
//  4. CommitToLedger      — ledger service owns the transaction internally.
//     All ledger errors (including insufficient balance) leave the record PENDING
//     and are retryable — balance state can change between attempts.
//  5. Mark PROCESSED      — terminal success state.
func (s *Service) Execute(ctx context.Context, req *CreateRequest) (*Transfer, error) {
	// ── Step 1: Idempotency / Resume ─────────────────────────────────────────
	var t *Transfer

	existing, err := s.repo.GetByTxnID(ctx, req.TxnID)
	if err != nil {
		// Technical failure looking up the record — surface and let caller retry.
		return nil, fmt.Errorf("transfer: idempotency lookup: %w", err)
	}

	if existing != nil {
		switch existing.Status {
		case StatusProcessed, StatusFailed:
			// Terminal state — return as-is; no further work needed.
			return existing, nil
		case StatusPending:
			// A prior attempt was interrupted mid-flight.
			// Resume the workflow using the data already committed to the DB.
			t = existing
		}
	} else {
		// No existing record — create a new PENDING entry.
		now := timeutil.Now()
		t = &Transfer{
			ID:           NewID(), // "txn_<uuidv7>"
			TxnID:        req.TxnID,
			FromWalletID: req.FromWalletID,
			ToWalletID:   req.ToWalletID,
			Amount:       req.Amount,
			Status:       StatusPending,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := s.repo.Create(ctx, t); err != nil {
			// Record does not exist yet — cannot mark FAILED; surface the error.
			return nil, fmt.Errorf("transfer: create pending record: %w", err)
		}
	}

	// markFailed transitions the transfer to FAILED — used for business validation
	// errors that are deterministic and should not be retried (e.g. bad amount,
	// inactive wallet).
	markFailed := func(reason string) (*Transfer, error) {
		_ = t.MarkFailed(reason)
		_ = s.repo.UpdateStatus(ctx, t.ID, StatusFailed, reason)
		return t, errors.New(reason)
	}

	// markRetryable leaves the transfer in PENDING and surfaces the error
	// so the caller knows to retry. The DB record is intentionally not touched
	// so the next call can resume from this point.
	markRetryable := func(err error) (*Transfer, error) {
		return t, fmt.Errorf("transfer: technical failure (retryable): %w", err)
	}

	// ── Step 2: Validate transfer entity ─────────────────────────────────────
	// Catches: invalid amount (<= 0), same source/destination wallet, missing fields.
	// These are business rule violations — mark FAILED permanently.
	if err := t.Validate(); err != nil {
		return markFailed(err.Error())
	}

	// ── Step 3: Validate wallets ─────────────────────────────────────────────
	// Single SQL query: WHERE id = ANY($1) AND status = 'ACTIVE'
	// Catches: wallet not found, wallet inactive — business failures → FAILED.
	if err := s.walletRepo.ValidateWallets(ctx, []string{t.FromWalletID, t.ToWalletID}); err != nil {
		return markFailed(fmt.Sprintf("wallet validation failed: %v", err))
	}

	// ── Step 4: CommitToLedger ───────────────────────────────────────────────
	// LedgerService owns BeginTx / Commit / Rollback internally.
	// No transaction object crosses domain boundaries.
	// All ledger errors (including ErrInsufficientBalance) are retryable —
	// balance state can change between retry attempts.
	if err := s.ledgerSvc.CommitToLedger(ctx, t.ID, t.FromWalletID, t.ToWalletID, t.Amount); err != nil {
		return markRetryable(err)
	}

	// ── Step 5: Mark PROCESSED ───────────────────────────────────────────────
	_ = t.MarkProcessed()
	_ = s.repo.UpdateStatus(ctx, t.ID, StatusProcessed, "")
	return t, nil
}

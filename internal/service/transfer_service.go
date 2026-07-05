// Package service contains the business logic for wallet transfers:
// idempotency handling, validation, and the debit/credit/ledger workflow.
// It depends only on the repository interfaces, so it's fully testable
// against the in-memory fake without a real database.
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"

	"github.com/example/wallet-transfer/internal/domain"
	"github.com/example/wallet-transfer/internal/idgen"
	"github.com/example/wallet-transfer/internal/repository"
)

// CreateTransferRequest mirrors the POST /transfers JSON body.
type CreateTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

// TransferResult is what CreateTransfer returns and also what gets cached
// against the idempotencyKey for replay on duplicate requests.
type TransferResult struct {
	TransferID    string               `json:"transferId"`
	State         domain.TransferState `json:"state"`
	FromWalletID  string               `json:"fromWalletId"`
	ToWalletID    string               `json:"toWalletId"`
	Amount        int64                `json:"amount"`
	FailureReason string               `json:"failureReason,omitempty"`
}

type TransferService struct {
	store repository.TxStore
}

func NewTransferService(store repository.TxStore) *TransferService {
	return &TransferService{store: store}
}

// CreateTransfer executes a wallet transfer with exactly-once semantics per
// idempotencyKey.
//
// High-level flow:
//  1. Validate the request shape (cheap checks, no DB access).
//  2. Open a single DB transaction for the whole operation.
//  3. Try to atomically claim the idempotencyKey. If someone already claimed
//     it, replay their cached result instead of doing any work.
//  4. Execute the transfer: lock both wallets, check the balance, move the
//     money, write the ledger, update the transfer's state.
//  5. Record the outcome against the idempotencyKey and commit.
//
// If anything unexpected fails (step 2 or 4 for infra reasons), the whole
// transaction is rolled back, including the idempotency claim — so the
// caller can safely retry with the same key. Deterministic business failures
// (like insufficient funds) are instead committed as a FAILED result, so
// retries return the same answer instead of re-locking wallets forever.
func (s *TransferService) CreateTransfer(ctx context.Context, req CreateTransferRequest) (TransferResult, error) {
	if err := validate(req); err != nil {
		return TransferResult{}, err
	}

	requestHash := hashRequest(req)

	tx, err := s.store.BeginTx(ctx)
	if err != nil {
		return TransferResult{}, err
	}
	defer tx.Rollback() // no-op if Commit already succeeded

	claimed, err := tx.ClaimIdempotencyKey(ctx, req.IdempotencyKey, requestHash)
	if err != nil {
		return TransferResult{}, err
	}
	if !claimed {
		return s.replayExisting(ctx, tx, req.IdempotencyKey, requestHash)
	}

	result, err := s.executeTransfer(ctx, tx, req)
	if err != nil {
		// Infra/unexpected error: let the deferred Rollback undo the claim too.
		return TransferResult{}, err
	}

	if err := s.finalize(ctx, tx, req.IdempotencyKey, result); err != nil {
		return TransferResult{}, err
	}
	return result, nil
}

// validate performs cheap, stateless checks before touching the database.
func validate(req CreateTransferRequest) error {
	if req.IdempotencyKey == "" {
		return domain.ErrIdempotencyKeyRequired
	}
	if req.Amount <= 0 {
		return domain.ErrInvalidAmount
	}
	if req.FromWalletID == req.ToWalletID {
		return domain.ErrSameWallet
	}
	return nil
}

// replayExisting handles the case where the idempotencyKey was already
// claimed by an earlier (or concurrent) request.
func (s *TransferService) replayExisting(ctx context.Context, tx repository.Tx, key, requestHash string) (TransferResult, error) {
	existing, err := tx.GetIdempotencyRecord(ctx, key)
	if err != nil {
		return TransferResult{}, err
	}
	if existing.RequestHash != requestHash {
		// Same key, different payload — this is almost certainly a client
		// bug (accidental key reuse), not a legitimate retry, so we refuse
		// rather than silently doing the wrong thing.
		return TransferResult{}, domain.ErrIdempotencyKeyConflict
	}
	switch existing.Status {
	case repository.IdempotencyCompleted, repository.IdempotencyFailed:
		var cached TransferResult
		if err := json.Unmarshal(existing.ResponseBody, &cached); err != nil {
			return TransferResult{}, err
		}
		return cached, nil
	default: // IN_PROGRESS: another request is mid-flight right now
		return TransferResult{}, domain.ErrDuplicateInFlight
	}
}

// finalize records the outcome against the idempotencyKey and commits the
// whole unit of work.
func (s *TransferService) finalize(ctx context.Context, tx repository.Tx, key string, result TransferResult) error {
	body, err := json.Marshal(result)
	if err != nil {
		return err
	}
	status := repository.IdempotencyCompleted
	if result.State == domain.TransferFailed {
		status = repository.IdempotencyFailed
	}
	if err := tx.CompleteIdempotencyKey(ctx, key, status, result.TransferID, body); err != nil {
		return err
	}
	return tx.Commit()
}

// executeTransfer does the actual money movement inside the already-open
// transaction. It assumes the idempotencyKey has already been claimed.
func (s *TransferService) executeTransfer(ctx context.Context, tx repository.Tx, req CreateTransferRequest) (TransferResult, error) {
	transferID := idgen.New("txn")

	if err := tx.InsertTransfer(ctx, domain.Transfer{
		ID:             transferID,
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		State:          domain.TransferPending,
	}); err != nil {
		return TransferResult{}, err
	}

	from, to, err := lockBothWallets(ctx, tx, req.FromWalletID, req.ToWalletID)
	if err != nil {
		return TransferResult{}, err
	}

	if from.Balance < req.Amount {
		return s.failTransfer(ctx, tx, transferID, req, domain.ErrInsufficientFunds.Error())
	}

	if err := tx.UpdateWalletBalance(ctx, from.ID, from.Balance-req.Amount); err != nil {
		return TransferResult{}, err
	}
	if err := tx.UpdateWalletBalance(ctx, to.ID, to.Balance+req.Amount); err != nil {
		return TransferResult{}, err
	}

	entries := []domain.LedgerEntry{
		{ID: idgen.New("led"), WalletID: from.ID, TransferID: transferID, Type: domain.LedgerDebit, Amount: req.Amount},
		{ID: idgen.New("led"), WalletID: to.ID, TransferID: transferID, Type: domain.LedgerCredit, Amount: req.Amount},
	}
	if err := tx.InsertLedgerEntries(ctx, entries); err != nil {
		return TransferResult{}, err
	}

	if err := tx.UpdateTransferState(ctx, transferID, domain.TransferProcessed, ""); err != nil {
		return TransferResult{}, err
	}

	return TransferResult{
		TransferID:   transferID,
		State:        domain.TransferProcessed,
		FromWalletID: req.FromWalletID,
		ToWalletID:   req.ToWalletID,
		Amount:       req.Amount,
	}, nil
}

// lockBothWallets locks the two wallets involved in a transfer, always in
// ascending ID order. This ordering is what prevents deadlocks: if two
// transfers run concurrently between the same pair of wallets in opposite
// directions, both will try to acquire locks in the same order and one will
// simply wait for the other, rather than each holding one lock the other
// needs.
func lockBothWallets(ctx context.Context, tx repository.Tx, fromID, toID string) (from, to domain.Wallet, err error) {
	ids := []string{fromID, toID}
	sort.Strings(ids)

	locked := make(map[string]domain.Wallet, 2)
	for _, id := range ids {
		w, err := tx.GetWalletForUpdate(ctx, id)
		if err != nil {
			return domain.Wallet{}, domain.Wallet{}, err
		}
		locked[id] = w
	}
	return locked[fromID], locked[toID], nil
}

// failTransfer marks a transfer FAILED for a deterministic business reason
// (as opposed to returning an error, which would trigger a full rollback and
// allow the idempotencyKey to be reused — not what we want for a failure
// that will always happen again given the same inputs).
func (s *TransferService) failTransfer(ctx context.Context, tx repository.Tx, transferID string, req CreateTransferRequest, reason string) (TransferResult, error) {
	if err := tx.UpdateTransferState(ctx, transferID, domain.TransferFailed, reason); err != nil {
		return TransferResult{}, err
	}
	return TransferResult{
		TransferID:    transferID,
		State:         domain.TransferFailed,
		FromWalletID:  req.FromWalletID,
		ToWalletID:    req.ToWalletID,
		Amount:        req.Amount,
		FailureReason: reason,
	}, nil
}

// hashRequest fingerprints the business-relevant fields of a request so we
// can detect a client reusing the same idempotencyKey for a different
// transfer (a client bug we want to reject loudly, not silently misroute).
func hashRequest(req CreateTransferRequest) string {
	h := sha256.Sum256([]byte(req.FromWalletID + "|" + req.ToWalletID + "|" + strconv.FormatInt(req.Amount, 10)))
	return hex.EncodeToString(h[:])
}

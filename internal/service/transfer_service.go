package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/karthikadarvin/wallet-transfer-assignment/internal/domain"
	"github.com/karthikadarvin/wallet-transfer-assignment/internal/repository"
	"github.com/karthikadarvin/wallet-transfer-assignment/internal/repository/postgres"
)

type TransferInput struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

type TransferResult struct {
	TransferID    string `json:"transferId"`
	FromWalletID  string `json:"fromWalletId"`
	ToWalletID    string `json:"toWalletId"`
	Amount        int64  `json:"amount"`
	Status        string `json:"status"`
	FailureReason string `json:"failureReason,omitempty"`
}

type TransferService struct {
	db          *sql.DB
	wallets     repository.WalletRepository
	transfers   repository.TransferRepository
	ledger      repository.LedgerRepository
	idempotency repository.IdempotencyRepository
}

func NewTransferService(
	db *sql.DB,
	wallets repository.WalletRepository,
	transfers repository.TransferRepository,
	ledger repository.LedgerRepository,
	idempotency repository.IdempotencyRepository,
) *TransferService {
	return &TransferService{
		db:          db,
		wallets:     wallets,
		transfers:   transfers,
		ledger:      ledger,
		idempotency: idempotency,
	}
}

func (s *TransferService) ExecuteTransfer(ctx context.Context, input TransferInput) (*TransferResult, error) {
	if input.IdempotencyKey == "" {
		return nil, domain.ErrIdempotencyKeyRequired
	}

	fingerprint := fingerprintFor(input)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // safe no-op if already committed

	// Step 1: reserve the idempotency key. A unique constraint violation
	// means a duplicate request (in-flight or already completed).
	_, err = s.idempotency.Insert(ctx, tx, input.IdempotencyKey, fingerprint)
	if err != nil {
		if errors.Is(err, postgres.ErrKeyAlreadyExists) {
			return s.resolveExistingKey(ctx, input.IdempotencyKey, fingerprint)
		}
		return nil, fmt.Errorf("reserve idempotency key: %w", err)
	}

	// Step 2: lock wallets in a consistent order to avoid deadlocks when
	// two transfers move money between the same pair of wallets.
	firstID, secondID := lockOrder(input.FromWalletID, input.ToWalletID)

	firstWallet, err := s.wallets.GetForUpdate(ctx, tx, firstID)
	if err != nil {
		return nil, err
	}
	secondWallet, err := s.wallets.GetForUpdate(ctx, tx, secondID)
	if err != nil {
		return nil, err
	}

	var fromWallet, toWallet *domain.Wallet
	if firstWallet.ID == input.FromWalletID {
		fromWallet, toWallet = firstWallet, secondWallet
	} else {
		fromWallet, toWallet = secondWallet, firstWallet
	}

	// Step 3: construct the transfer (validates amount + distinct wallets).
	transfer, err := domain.NewTransfer(input.FromWalletID, input.ToWalletID, input.Amount)
	if err != nil {
		return nil, err
	}

	if err := s.transfers.Create(ctx, tx, transfer); err != nil {
		return nil, fmt.Errorf("create transfer: %w", err)
	}

	// Step 4: balance check. Insufficient funds is a real, valid outcome —
	// not an error path — it resolves the transfer to FAILED and completes
	// the idempotency record so retries return the same failed result.
	if err := fromWallet.CanDebit(input.Amount); err != nil {
		if errors.Is(err, domain.ErrInsufficientBalance) {
			return s.finalizeFailed(ctx, tx, transfer, input.IdempotencyKey, "INSUFFICIENT_BALANCE")
		}
		return nil, err
	}

	// Step 5: apply balance changes.
	if err := fromWallet.Debit(input.Amount); err != nil {
		return nil, err
	}
	if err := toWallet.Credit(input.Amount); err != nil {
		return nil, err
	}

	if err := s.wallets.UpdateBalance(ctx, tx, fromWallet.ID, fromWallet.Balance); err != nil {
		return nil, fmt.Errorf("update from wallet: %w", err)
	}
	if err := s.wallets.UpdateBalance(ctx, tx, toWallet.ID, toWallet.Balance); err != nil {
		return nil, fmt.Errorf("update to wallet: %w", err)
	}

	// Step 6: double-entry ledger.
	debit, credit := domain.NewLedgerEntryPair(transfer.ID, transfer.FromWalletID, transfer.ToWalletID, transfer.Amount)
	if err := s.ledger.InsertPair(ctx, tx, debit, credit); err != nil {
		return nil, fmt.Errorf("insert ledger entries: %w", err)
	}

	// Step 7: transition transfer to PROCESSED.
	if err := transfer.MarkProcessed(); err != nil {
		return nil, err
	}
	if err := s.transfers.UpdateStatus(ctx, tx, transfer.ID, transfer.Status, ""); err != nil {
		return nil, fmt.Errorf("update transfer status: %w", err)
	}

	result := &TransferResult{
		TransferID:   transfer.ID,
		FromWalletID: transfer.FromWalletID,
		ToWalletID:   transfer.ToWalletID,
		Amount:       transfer.Amount,
		Status:       string(transfer.Status),
	}

	// Step 8: complete the idempotency record with the cached response.
	snapshot, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal response snapshot: %w", err)
	}
	if err := s.idempotency.Complete(ctx, tx, input.IdempotencyKey, transfer.ID, snapshot); err != nil {
		return nil, fmt.Errorf("complete idempotency record: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return result, nil
}

// finalizeFailed persists a FAILED transfer and completes the idempotency
// record within the same transaction, so the failure itself is the durable,
// idempotent outcome — a retry returns this same result, it doesn't re-attempt.
func (s *TransferService) finalizeFailed(ctx context.Context, tx *sql.Tx, transfer *domain.Transfer, idempotencyKey, reason string) (*TransferResult, error) {
	if err := transfer.MarkFailed(reason); err != nil {
		return nil, err
	}
	if err := s.transfers.UpdateStatus(ctx, tx, transfer.ID, transfer.Status, transfer.FailureReason); err != nil {
		return nil, fmt.Errorf("update transfer status: %w", err)
	}

	result := &TransferResult{
		TransferID:    transfer.ID,
		FromWalletID:  transfer.FromWalletID,
		ToWalletID:    transfer.ToWalletID,
		Amount:        transfer.Amount,
		Status:        string(transfer.Status),
		FailureReason: transfer.FailureReason,
	}

	snapshot, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal response snapshot: %w", err)
	}
	if err := s.idempotency.Complete(ctx, tx, idempotencyKey, transfer.ID, snapshot); err != nil {
		return nil, fmt.Errorf("complete idempotency record: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return result, nil
}

// resolveExistingKey handles the case where the idempotency key already
// exists: either a completed prior request (return cached result) or a
// genuine concurrent duplicate still in flight (return a conflict error).
func (s *TransferService) resolveExistingKey(ctx context.Context, key, fingerprint string) (*TransferResult, error) {
	rec, err := s.idempotency.GetByKey(ctx, s.db, key)
	if err != nil {
		return nil, fmt.Errorf("lookup idempotency record: %w", err)
	}

	if rec.RequestFingerprint != fingerprint {
		return nil, domain.ErrIdempotencyKeyConflict
	}

	switch rec.Status {
	case domain.IdempotencyStatusCompleted:
		var result TransferResult
		if err := json.Unmarshal(rec.ResponseSnapshot, &result); err != nil {
			return nil, fmt.Errorf("unmarshal cached response: %w", err)
		}
		return &result, nil
	default: // IN_PROGRESS
		return nil, domain.ErrIdempotencyInProgress
	}
}

// lockOrder returns the two wallet IDs in a deterministic order so that
// any two transfers touching the same pair of wallets always acquire
// row locks in the same sequence, preventing deadlocks.
func lockOrder(a, b string) (first, second string) {
	ids := []string{a, b}
	sort.Strings(ids)
	return ids[0], ids[1]
}

func fingerprintFor(input TransferInput) string {
	raw := fmt.Sprintf("%s|%s|%d", input.FromWalletID, input.ToWalletID, input.Amount)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

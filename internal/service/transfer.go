package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/benjamin-benny/wallet-transfer/internal/domain"
	"github.com/benjamin-benny/wallet-transfer/internal/repository"
)

// TransferResponse is the canonical response body for a transfer operation.
// It is stored in the idempotency record so replays return identical JSON.
type TransferResponse struct {
	TransferID   string  `json:"transferId"`
	Status       string  `json:"status"`
	FromWalletID string  `json:"fromWalletId"`
	ToWalletID   string  `json:"toWalletId"`
	Amount       string  `json:"amount"`
	CreatedAt    string  `json:"createdAt"`
	ProcessedAt  *string `json:"processedAt"`
}

// CreateResult carries the response and the HTTP status the handler should emit.
type CreateResult struct {
	Response   *TransferResponse
	HTTPStatus int
	// IsReplay is true when the request matched a stored idempotency record.
	IsReplay bool
}

// TransferService orchestrates the transfer business logic.
type TransferService struct {
	pool        *pgxpool.Pool
	wallets     *repository.WalletRepository
	transfers   *repository.TransferRepository
	ledger      *repository.LedgerRepository
	idempotency *repository.IdempotencyRepository
}

func NewTransferService(pool *pgxpool.Pool) *TransferService {
	return &TransferService{
		pool:        pool,
		wallets:     &repository.WalletRepository{},
		transfers:   &repository.TransferRepository{},
		ledger:      &repository.LedgerRepository{},
		idempotency: &repository.IdempotencyRepository{},
	}
}

// CreateWallet is a convenience method for tests and manual setup.
func (s *TransferService) CreateWallet(ctx context.Context, currency string, initialBalance domain.Amount) (*domain.Wallet, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	w, err := s.wallets.Create(ctx, tx, currency, initialBalance)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return w, nil
}

// Create executes the full transfer flow atomically.
//
// The 14-step flow (per spec):
//  1. Validate request
//  2. BEGIN TRANSACTION
//  3. INSERT idempotency record ON CONFLICT DO NOTHING
//  4. On conflict: validate hash, return stored response
//  5. Lock both wallets FOR UPDATE in sorted UUID order
//  6. Verify both wallets exist
//  7. Check balance; on insufficiency: record FAILED transfer, COMMIT, return 422
//  8. INSERT transfer (PENDING)
//  9. INSERT ledger entries (DEBIT from, CREDIT to)
//  10. UPDATE wallet balances
//  11. UPDATE transfer → PROCESSED
//  12. UPDATE idempotency record with final response
//  13. COMMIT
//  14. Return response
func (s *TransferService) Create(ctx context.Context, req domain.CreateTransferRequest) (*CreateResult, error) {
	// Step 1: domain validation
	if err := req.Validate(); err != nil {
		return nil, err
	}

	requestHash, err := hashRequest(req)
	if err != nil {
		return nil, fmt.Errorf("hash request: %w", err)
	}

	// Step 2: begin transaction
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Step 3 & 4: idempotency check
	idem, inserted, err := s.idempotency.Insert(ctx, tx, req.IdempotencyKey, requestHash)
	if err != nil {
		return nil, fmt.Errorf("idempotency check: %w", err)
	}

	if !inserted {
		// Step 4: key already exists — hash mismatch is a hard conflict; no commit needed,
		// the deferred rollback cleans up the read-only transaction.
		if idem.RequestHash != requestHash {
			return nil, domain.ErrIdempotencyConflict
		}
		// Hash matches — commit the read-only tx and replay the stored response.
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit replay tx: %w", err)
		}
		var resp TransferResponse
		if err := json.Unmarshal(idem.ResponseBody, &resp); err != nil {
			return nil, fmt.Errorf("unmarshal stored response: %w", err)
		}
		return &CreateResult{
			Response:   &resp,
			HTTPStatus: idem.ResponseStatus,
			IsReplay:   true,
		}, nil
	}

	// Step 5: lock both wallets in sorted UUID order (deadlock prevention)
	sortedIDs := sortedUUIDs(req.FromWalletID, req.ToWalletID)
	wallets, err := s.wallets.GetByIDsForUpdate(ctx, tx, sortedIDs)
	if err != nil {
		return nil, fmt.Errorf("lock wallets: %w", err)
	}

	// Step 6: both wallets must exist
	if len(wallets) != 2 {
		return nil, domain.ErrWalletNotFound
	}
	walletByID := make(map[uuid.UUID]*domain.Wallet, 2)
	for _, w := range wallets {
		walletByID[w.ID] = w
	}
	fromWallet, fromOK := walletByID[req.FromWalletID]
	_, toOK := walletByID[req.ToWalletID]
	if !fromOK || !toOK {
		return nil, domain.ErrWalletNotFound
	}

	// Step 7: insufficient funds — still a committed business outcome
	if !fromWallet.Balance.GTE(req.Amount) {
		return s.handleInsufficientFunds(ctx, tx, req)
	}
	return s.executeSuccessfulTransfer(ctx, tx, req, fromWallet, walletByID[req.ToWalletID])
}

// handleInsufficientFunds records a FAILED transfer, stores the idempotency response,
// and commits. This is step 7 of the transfer flow.
func (s *TransferService) handleInsufficientFunds(
	ctx context.Context, tx pgx.Tx, req domain.CreateTransferRequest,
) (*CreateResult, error) {
	transfer := &domain.Transfer{
		FromWalletID: req.FromWalletID,
		ToWalletID:   req.ToWalletID,
		Amount:       req.Amount,
	}
	if err := s.transfers.Create(ctx, tx, transfer); err != nil {
		return nil, fmt.Errorf("create failed transfer: %w", err)
	}
	if err := s.transfers.MarkFailed(ctx, tx, transfer.ID, domain.ErrInsufficientFunds.Error()); err != nil {
		return nil, fmt.Errorf("mark transfer failed: %w", err)
	}
	transfer.Status = domain.StatusFailed

	resp := buildResponse(transfer)
	body, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("marshal response: %w", err)
	}
	if err := s.idempotency.UpdateResponse(ctx, tx, req.IdempotencyKey, transfer.ID, http.StatusUnprocessableEntity, body); err != nil {
		return nil, fmt.Errorf("update idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return &CreateResult{Response: resp, HTTPStatus: http.StatusUnprocessableEntity}, nil
}

// executeSuccessfulTransfer performs steps 8–13: inserts the transfer, ledger entries,
// updates balances, marks PROCESSED, persists the idempotency response, and commits.
func (s *TransferService) executeSuccessfulTransfer(
	ctx context.Context, tx pgx.Tx,
	req domain.CreateTransferRequest,
	fromWallet, toWallet *domain.Wallet,
) (*CreateResult, error) {
	// Step 8: insert transfer row with PENDING status
	transfer := &domain.Transfer{
		FromWalletID: req.FromWalletID,
		ToWalletID:   req.ToWalletID,
		Amount:       req.Amount,
	}
	if err := s.transfers.Create(ctx, tx, transfer); err != nil {
		return nil, fmt.Errorf("create transfer: %w", err)
	}

	// Step 9: double-entry ledger
	entries := []*domain.LedgerEntry{
		{TransferID: transfer.ID, WalletID: req.FromWalletID, EntryType: domain.EntryDebit, Amount: req.Amount},
		{TransferID: transfer.ID, WalletID: req.ToWalletID, EntryType: domain.EntryCredit, Amount: req.Amount},
	}
	if err := s.ledger.CreateEntries(ctx, tx, entries); err != nil {
		return nil, fmt.Errorf("create ledger entries: %w", err)
	}

	// Step 10: update balances
	if err := s.wallets.UpdateBalance(ctx, tx, req.FromWalletID, fromWallet.Balance.Sub(req.Amount)); err != nil {
		return nil, fmt.Errorf("update from balance: %w", err)
	}
	if err := s.wallets.UpdateBalance(ctx, tx, req.ToWalletID, toWallet.Balance.Add(req.Amount)); err != nil {
		return nil, fmt.Errorf("update to balance: %w", err)
	}

	// Step 11: mark PROCESSED
	processedAt := time.Now().UTC()
	if err := s.transfers.MarkProcessed(ctx, tx, transfer.ID, processedAt); err != nil {
		return nil, fmt.Errorf("mark processed: %w", err)
	}
	transfer.Status = domain.StatusProcessed
	transfer.ProcessedAt = &processedAt

	// Step 12: persist final response in idempotency record
	resp := buildResponse(transfer)
	body, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("marshal response: %w", err)
	}
	if err := s.idempotency.UpdateResponse(ctx, tx, req.IdempotencyKey, transfer.ID, http.StatusCreated, body); err != nil {
		return nil, fmt.Errorf("update idempotency: %w", err)
	}

	// Step 13: commit
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	// Step 14: return
	return &CreateResult{Response: resp, HTTPStatus: http.StatusCreated}, nil
}

// hashRequest computes SHA256 over the canonical JSON of the request payload
// (everything except the idempotency key itself).
func hashRequest(req domain.CreateTransferRequest) (string, error) {
	// Use a struct with sorted keys to get a deterministic JSON encoding.
	payload := struct {
		Amount       string `json:"amount"`
		FromWalletID string `json:"fromWalletId"`
		ToWalletID   string `json:"toWalletId"`
	}{
		Amount:       req.Amount.String(),
		FromWalletID: req.FromWalletID.String(),
		ToWalletID:   req.ToWalletID.String(),
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// sortedUUIDs returns [a, b] ordered by byte value, matching Postgres ORDER BY on uuid.
func sortedUUIDs(a, b uuid.UUID) []uuid.UUID {
	if bytes.Compare(a[:], b[:]) < 0 {
		return []uuid.UUID{a, b}
	}
	return []uuid.UUID{b, a}
}

func buildResponse(t *domain.Transfer) *TransferResponse {
	resp := &TransferResponse{
		TransferID:   t.ID.String(),
		Status:       string(t.Status),
		FromWalletID: t.FromWalletID.String(),
		ToWalletID:   t.ToWalletID.String(),
		Amount:       t.Amount.String(),
		CreatedAt:    t.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if t.ProcessedAt != nil {
		s := t.ProcessedAt.UTC().Format(time.RFC3339Nano)
		resp.ProcessedAt = &s
	}
	return resp
}

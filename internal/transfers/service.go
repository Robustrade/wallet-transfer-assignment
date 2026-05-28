package transfers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-transfer-assignment/internal/idempotency"
)

type WalletTxRepository interface {
	LockTx(ctx context.Context, tx pgx.Tx, walletID string) (bool, error)
	DebitIfSufficientTx(ctx context.Context, tx pgx.Tx, walletID string, amount int64) (bool, error)
	CreditTx(ctx context.Context, tx pgx.Tx, walletID string, amount int64) error
}

type TransferTxRepository interface {
	InsertTx(ctx context.Context, tx pgx.Tx, t Transfer) error
	UpdateStateTx(ctx context.Context, tx pgx.Tx, transferID string, state State, failureReason string) error
}

type LedgerTxRepository interface {
	InsertEntriesTx(ctx context.Context, tx pgx.Tx, transferID string, fromWalletID string, toWalletID string, amount int64) error
}

type ServiceDeps struct {
	Wallets     WalletTxRepository
	Transfers   TransferTxRepository
	Ledger      LedgerTxRepository
	Idempotency idempotency.Store
	DB          *pgxpool.Pool
}

type service struct {
	wallets     WalletTxRepository
	transfers   TransferTxRepository
	ledger      LedgerTxRepository
	idempotency idempotency.Store
	db          *pgxpool.Pool
}

func NewService(deps ServiceDeps) Service {
	return &service{
		wallets:     deps.Wallets,
		transfers:   deps.Transfers,
		ledger:      deps.Ledger,
		idempotency: deps.Idempotency,
		db:          deps.DB,
	}
}

type finalizedCache interface {
	CacheFinalizedResponse(key idempotency.Key, requestHash string, status int, body json.RawMessage)
}

func (s *service) Create(ctx context.Context, req CreateRequest) (int, json.RawMessage, error) {
	if err := validateCreate(req); err != nil {
		return http.StatusBadRequest, nil, err
	}

	key := idempotency.Key(req.IdempotencyKey)
	requestHash := idempotency.HashTransfer(req.FromWalletID, req.ToWalletID, req.Amount)

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return http.StatusInternalServerError, nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	adaptedTx := idempotency.AdaptPgxTx(tx)

	inserted, existing, err := s.idempotency.TryInsert(ctx, adaptedTx, key, requestHash)
	if err != nil {
		return http.StatusInternalServerError, nil, fmt.Errorf("idempotency insert: %w", err)
	}
	if !inserted {
		if existing.RequestHash != requestHash {
			return http.StatusConflict, mustJSON(map[string]any{"error": "idempotency key reused with different payload"}), ErrIdempotencyConflict
		}
		if existing.Ready {
			return existing.StatusCode, existing.Body, nil
		}
		return http.StatusInternalServerError, nil, fmt.Errorf("idempotency record not finalized")
	}

	// Lock wallets BEFORE inserting the transfer row so FK violations can't
	// occur and we can return a clean 404 when a wallet doesn't exist.
	firstID, secondID := lockOrder(req.FromWalletID, req.ToWalletID)
	firstOK, err := s.wallets.LockTx(ctx, tx, firstID)
	if err != nil {
		return http.StatusInternalServerError, nil, fmt.Errorf("lock wallet: %w", err)
	}
	secondOK, err := s.wallets.LockTx(ctx, tx, secondID)
	if err != nil {
		return http.StatusInternalServerError, nil, fmt.Errorf("lock wallet: %w", err)
	}
	if !firstOK || !secondOK {
		body := mustJSON(map[string]any{"error": "wallet not found"})
		_ = s.idempotency.SetResponse(ctx, adaptedTx, key, requestHash, http.StatusNotFound, body)
		if err := tx.Commit(ctx); err != nil {
			return http.StatusInternalServerError, nil, fmt.Errorf("commit: %w", err)
		}
		if c, ok := s.idempotency.(finalizedCache); ok {
			c.CacheFinalizedResponse(key, requestHash, http.StatusNotFound, body)
		}
		return http.StatusNotFound, body, ErrWalletNotFound
	}

	// Both wallets confirmed present — now it's safe to insert the transfer row.
	transferID := uuid.NewString()
	transfer := Transfer{
		ID:             transferID,
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		State:          StatePending,
	}
	if err := s.transfers.InsertTx(ctx, tx, transfer); err != nil {
		return http.StatusInternalServerError, nil, fmt.Errorf("insert transfer: %w", err)
	}

	debited, err := s.wallets.DebitIfSufficientTx(ctx, tx, req.FromWalletID, req.Amount)
	if err != nil {
		return http.StatusInternalServerError, nil, fmt.Errorf("debit: %w", err)
	}
	if !debited {
		_ = s.transfers.UpdateStateTx(ctx, tx, transferID, StateFailed, "INSUFFICIENT_FUNDS")
		body := mustJSON(map[string]any{
			"transferId": transferID,
			"state":      StateFailed,
			"error":      "insufficient funds",
		})
		_ = s.idempotency.SetResponse(ctx, adaptedTx, key, requestHash, http.StatusConflict, body)
		if err := tx.Commit(ctx); err != nil {
			return http.StatusInternalServerError, nil, fmt.Errorf("commit: %w", err)
		}
		if c, ok := s.idempotency.(finalizedCache); ok {
			c.CacheFinalizedResponse(key, requestHash, http.StatusConflict, body)
		}
		return http.StatusConflict, body, ErrInsufficientFunds
	}

	if err := s.wallets.CreditTx(ctx, tx, req.ToWalletID, req.Amount); err != nil {
		return http.StatusInternalServerError, nil, fmt.Errorf("credit: %w", err)
	}

	if err := s.ledger.InsertEntriesTx(ctx, tx, transferID, req.FromWalletID, req.ToWalletID, req.Amount); err != nil {
		return http.StatusInternalServerError, nil, fmt.Errorf("insert ledger: %w", err)
	}

	if err := s.transfers.UpdateStateTx(ctx, tx, transferID, StateProcessed, ""); err != nil {
		return http.StatusInternalServerError, nil, fmt.Errorf("update transfer: %w", err)
	}

	body := mustJSON(CreateResponse{TransferID: transferID, State: StateProcessed})
	if err := s.idempotency.SetResponse(ctx, adaptedTx, key, requestHash, http.StatusCreated, body); err != nil {
		return http.StatusInternalServerError, nil, fmt.Errorf("store idempotency response: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return http.StatusInternalServerError, nil, fmt.Errorf("commit: %w", err)
	}
	if c, ok := s.idempotency.(finalizedCache); ok {
		c.CacheFinalizedResponse(key, requestHash, http.StatusCreated, body)
	}

	return http.StatusCreated, body, nil
}

func validateCreate(req CreateRequest) error {
	if req.IdempotencyKey == "" || req.FromWalletID == "" || req.ToWalletID == "" {
		return fmt.Errorf("%w: missing required fields", ErrInvalidRequest)
	}
	if req.FromWalletID == req.ToWalletID {
		return fmt.Errorf("%w: fromWalletId and toWalletId must differ", ErrInvalidRequest)
	}
	if req.Amount <= 0 {
		return fmt.Errorf("%w: amount must be > 0", ErrInvalidRequest)
	}
	return nil
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func lockOrder(a, b string) (string, string) {
	if a <= b {
		return a, b
	}
	return b, a
}

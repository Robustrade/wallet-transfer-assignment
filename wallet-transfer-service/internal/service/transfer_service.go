package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Swapnil811/wallet-transfer-assignment/wallet-transfer-service/internal/domain"
	"github.com/Swapnil811/wallet-transfer-assignment/wallet-transfer-service/internal/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TransferService contains the business workflow for wallet transfers.
// A transfer's balance changes, ledger entries, state transition, and
// idempotency record are committed in the same PostgreSQL transaction.
type TransferService struct {
	pool  *pgxpool.Pool
	repos *repository.Repositories
}

func NewTransferService(pool *pgxpool.Pool, repos *repository.Repositories) *TransferService {
	return &TransferService{pool: pool, repos: repos}
}

type TransferResult struct {
	Transfer domain.Transfer
	Replayed bool
}

// CreateTransfer executes a wallet transfer with API-level idempotency.
//
// Important concurrency properties:
//   - source and destination wallet rows are locked FOR UPDATE
//   - locks are always acquired in UUID order to avoid deadlocks
//   - all transfer side effects are committed atomically
//   - a duplicate idempotency key returns the original transfer
func (s *TransferService) CreateTransfer(ctx context.Context, req domain.CreateTransferRequest) (TransferResult, error) {
	if err := validateRequest(req); err != nil {
		return TransferResult{}, err
	}

	hash, err := requestHash(req)
	if err != nil {
		return TransferResult{}, fmt.Errorf("hash transfer request: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TransferResult{}, fmt.Errorf("begin transfer transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	transfer := domain.Transfer{
		ID:           uuid.New(),
		FromWalletID: req.FromWalletID,
		ToWalletID:   req.ToWalletID,
		Amount:       req.Amount,
		Status:       domain.TransferPending,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}

	// Create the transfer first because idempotency_records.transfer_id has a
	// foreign key to transfers. If another request wins the same idempotency
	// key concurrently, the INSERT below will block until that transaction
	// commits, then return a unique-constraint error. We roll back this whole
	// transaction and fetch the winner's result outside the transaction.
	if err := s.repos.Transfer.Create(ctx, tx, transfer); err != nil {
		return TransferResult{}, err
	}

	rec := repository.IdempotencyRecord{
		Key:         req.IdempotencyKey,
		RequestHash: hash,
		TransferID:  transfer.ID,
	}

	if err := s.repos.Idempotency.Create(ctx, tx, rec); err != nil {
		if isUniqueViolation(err) {
			if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
				return TransferResult{}, fmt.Errorf("rollback duplicate transfer: %w", rollbackErr)
			}
			return s.replayExisting(ctx, req.IdempotencyKey, hash)
		}
		return TransferResult{}, err
	}

	wallets := []uuid.UUID{req.FromWalletID, req.ToWalletID}
	sort.Slice(wallets, func(i, j int) bool { return wallets[i].String() < wallets[j].String() })

	locked := make(map[uuid.UUID]domain.Wallet, 2)
	for _, walletID := range wallets {
		wallet, err := s.repos.Wallet.GetForUpdate(ctx, tx, walletID)
		if err != nil {
			return TransferResult{}, err
		}
		locked[walletID] = wallet
	}

	source := locked[req.FromWalletID]
	destination := locked[req.ToWalletID]

	if source.Balance < req.Amount {
		if err := s.repos.Transfer.UpdateStatus(ctx, tx, transfer.ID, domain.TransferPending, domain.TransferFailed); err != nil {
			return TransferResult{}, err
		}
		transfer.Status = domain.TransferFailed
		transfer.UpdatedAt = time.Now().UTC()

		if err := tx.Commit(ctx); err != nil {
			return TransferResult{}, fmt.Errorf("commit failed transfer: %w", err)
		}
		return TransferResult{Transfer: transfer}, nil
	}

	if err := s.repos.Wallet.UpdateBalance(ctx, tx, source.ID, source.Balance-req.Amount); err != nil {
		return TransferResult{}, err
	}
	if err := s.repos.Wallet.UpdateBalance(ctx, tx, destination.ID, destination.Balance+req.Amount); err != nil {
		return TransferResult{}, err
	}

	debit := domain.LedgerEntry{
		ID:         uuid.New(),
		TransferID: transfer.ID,
		WalletID:   source.ID,
		Type:       domain.LedgerDebit,
		Amount:     req.Amount,
	}
	credit := domain.LedgerEntry{
		ID:         uuid.New(),
		TransferID: transfer.ID,
		WalletID:   destination.ID,
		Type:       domain.LedgerCredit,
		Amount:     req.Amount,
	}

	if err := s.repos.Ledger.Create(ctx, tx, debit); err != nil {
		return TransferResult{}, err
	}
	if err := s.repos.Ledger.Create(ctx, tx, credit); err != nil {
		return TransferResult{}, err
	}

	if err := s.repos.Transfer.UpdateStatus(ctx, tx, transfer.ID, domain.TransferPending, domain.TransferProcessed); err != nil {
		return TransferResult{}, err
	}
	transfer.Status = domain.TransferProcessed
	transfer.UpdatedAt = time.Now().UTC()

	if err := tx.Commit(ctx); err != nil {
		return TransferResult{}, fmt.Errorf("commit transfer: %w", err)
	}

	return TransferResult{Transfer: transfer}, nil
}

func (s *TransferService) replayExisting(ctx context.Context, key, requestHash string) (TransferResult, error) {
	rec, err := s.repos.Idempotency.Get(ctx, s.pool, key)
	if err != nil {
		return TransferResult{}, fmt.Errorf("get existing idempotency record: %w", err)
	}
	if err := repository.CompareRequest(rec, requestHash); err != nil {
		return TransferResult{}, err
	}

	transfer, err := s.repos.Transfer.GetByID(ctx, s.pool, rec.TransferID)
	if err != nil {
		return TransferResult{}, fmt.Errorf("get original transfer: %w", err)
	}
	return TransferResult{Transfer: transfer, Replayed: true}, nil
}

func validateRequest(req domain.CreateTransferRequest) error {
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		return fmt.Errorf("%w: idempotency key is required", domain.ErrInvalidTransfer)
	}
	if req.FromWalletID == uuid.Nil || req.ToWalletID == uuid.Nil {
		return fmt.Errorf("%w: wallet IDs are required", domain.ErrInvalidTransfer)
	}
	if req.FromWalletID == req.ToWalletID {
		return fmt.Errorf("%w: source and destination wallets must differ", domain.ErrInvalidTransfer)
	}
	if req.Amount <= 0 {
		return fmt.Errorf("%w: amount must be greater than zero", domain.ErrInvalidTransfer)
	}
	return nil
}

func requestHash(req domain.CreateTransferRequest) (string, error) {
	// Deliberately exclude the idempotency key. The key identifies the logical
	// request; the hash verifies that the same key is not reused for different
	// transfer parameters.
	canonical := struct {
		FromWalletID string `json:"fromWalletId"`
		ToWalletID   string `json:"toWalletId"`
		Amount       int64  `json:"amount"`
	}{
		FromWalletID: req.FromWalletID.String(),
		ToWalletID:   req.ToWalletID.String(),
		Amount:       req.Amount,
	}

	b, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

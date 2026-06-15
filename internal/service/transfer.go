package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wallet-transfer-assignment/internal/domain"
	"github.com/wallet-transfer-assignment/internal/repository"
)

// TransferRequest carries the caller-supplied parameters for a new transfer.
type TransferRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

// transferRepos bundles the repositories the service needs.
// Keeping them together simplifies the constructor and testing.
type transferRepos struct {
	wallets  *repository.WalletRepository
	transfers *repository.TransferRepository
	ledger   *repository.LedgerEntryRepository
}

// TransferService orchestrates wallet-to-wallet transfers.
// It owns the transaction boundary and enforces idempotency and concurrency safety.
type TransferService struct {
	pool  *pgxpool.Pool
	repos transferRepos
}

func NewTransferService(
	pool *pgxpool.Pool,
	wallets *repository.WalletRepository,
	transfers *repository.TransferRepository,
	ledger *repository.LedgerEntryRepository,
) *TransferService {
	return &TransferService{
		pool: pool,
		repos: transferRepos{
			wallets:   wallets,
			transfers: transfers,
			ledger:    ledger,
		},
	}
}

// CreateTransfer executes a wallet transfer with exactly-once semantics.
//
// Idempotency: the idempotency key is stored with a UNIQUE constraint. If the
// same key arrives again, the service returns the original transfer record
// without executing any side effects.
//
// Concurrency: wallet rows are locked with SELECT FOR UPDATE inside a
// serializable-ish READ COMMITTED transaction. Lock acquisition always uses
// alphabetical wallet ID order to eliminate the possibility of deadlocks when
// two goroutines transfer between the same pair of wallets simultaneously.
func (s *TransferService) CreateTransfer(ctx context.Context, req TransferRequest) (*domain.Transfer, error) {
	// Build and validate the domain object before touching the database.
	transfer := &domain.Transfer{
		ID:             uuid.New(),
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		Status:         domain.StatusPending,
	}
	if err := transfer.Validate(); err != nil {
		return nil, err
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	// The deferred rollback is a no-op after a successful Commit.
	defer func() {
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			slog.Error("rollback failed", "err", rbErr)
		}
	}()

	// Scope all repository calls to the open transaction.
	txWallets := repository.NewWalletRepository(tx)
	txTransfers := repository.NewTransferRepository(tx)
	txLedger := repository.NewLedgerEntryRepository(tx)

	// Persist the transfer record first so the idempotency key is claimed.
	// If this fails with ErrDuplicateKey the request is a replay — return the
	// original result immediately without acquiring any wallet locks.
	if err := txTransfers.Create(ctx, transfer); err != nil {
		if errors.Is(err, domain.ErrDuplicateKey) {
			_ = tx.Rollback(ctx)
			return s.repos.transfers.GetByIdempotencyKey(ctx, req.IdempotencyKey)
		}
		return nil, fmt.Errorf("create transfer record: %w", err)
	}

	// Lock wallet rows in deterministic (alphabetical) order to prevent
	// deadlocks when concurrent requests transfer between the same pair.
	first, second := req.FromWalletID, req.ToWalletID
	if first > second {
		first, second = second, first
	}

	firstWallet, err := txWallets.GetByIDForUpdate(ctx, first)
	if err != nil {
		return s.failTransfer(ctx, tx, txTransfers, transfer, err)
	}
	secondWallet, err := txWallets.GetByIDForUpdate(ctx, second)
	if err != nil {
		return s.failTransfer(ctx, tx, txTransfers, transfer, err)
	}

	// Re-map back to semantic from/to after the ordered lock acquisition.
	fromWallet, toWallet := firstWallet, secondWallet
	if req.FromWalletID == second {
		fromWallet, toWallet = secondWallet, firstWallet
	}

	// Balance check happens after the lock is held, so the balance we read
	// is stable for the duration of this transaction.
	if fromWallet.Balance < req.Amount {
		return s.failTransfer(ctx, tx, txTransfers, transfer,
			fmt.Errorf("%w: have %d, need %d", domain.ErrInsufficientFunds, fromWallet.Balance, req.Amount))
	}

	// Apply balance changes.
	if err := txWallets.UpdateBalance(ctx, fromWallet.ID, -req.Amount, fromWallet.Version); err != nil {
		return s.failTransfer(ctx, tx, txTransfers, transfer, err)
	}
	if err := txWallets.UpdateBalance(ctx, toWallet.ID, req.Amount, toWallet.Version); err != nil {
		return s.failTransfer(ctx, tx, txTransfers, transfer, err)
	}

	// Write the two ledger entries atomically with everything else.
	debit, credit := domain.NewLedgerPair(transfer.ID, req.FromWalletID, req.ToWalletID, req.Amount)
	if err := txLedger.CreatePair(ctx, debit, credit); err != nil {
		return s.failTransfer(ctx, tx, txTransfers, transfer, err)
	}

	// All side effects succeeded — mark the transfer PROCESSED.
	if err := transfer.MarkProcessed(); err != nil {
		return s.failTransfer(ctx, tx, txTransfers, transfer, err)
	}
	if err := txTransfers.UpdateStatus(ctx, transfer); err != nil {
		return s.failTransfer(ctx, tx, txTransfers, transfer, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}
	return transfer, nil
}

// failTransfer marks the transfer as FAILED within the same transaction,
// then rolls back. It returns the (now-failed) transfer so the handler can
// respond with the terminal state rather than a bare error.
func (s *TransferService) failTransfer(
	ctx context.Context,
	tx pgx.Tx,
	txTransfers *repository.TransferRepository,
	transfer *domain.Transfer,
	cause error,
) (*domain.Transfer, error) {
	_ = transfer.MarkFailed(cause.Error())
	_ = txTransfers.UpdateStatus(ctx, transfer)
	_ = tx.Commit(ctx) // commit the FAILED status so it is durable
	return transfer, cause
}

// GetTransfer returns a single transfer by its ID, for optional read endpoints.
func (s *TransferService) GetByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error) {
	return s.repos.transfers.GetByIdempotencyKey(ctx, key)
}

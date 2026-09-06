package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jaiminbhaduri/wallet-transfer-assignment/internal/domain"
	"github.com/jaiminbhaduri/wallet-transfer-assignment/internal/repository/postgres"
)

type TransferService struct {
	pool *pgxpool.Pool
}

func NewTransferService(pool *pgxpool.Pool) *TransferService {
	return &TransferService{pool: pool}
}

// CreateTransfer executes a wallet-to-wallet transfer with exactly-once
// semantics under the given idempotency key.
//
// Concurrency strategy: both wallets are locked with SELECT ... FOR UPDATE
// inside a single DB transaction, always acquired in ascending wallet-id
// order, and — critically — *before* the transfer row is inserted. Fixed
// lock ordering alone prevents deadlocks between two transfers racing for
// the same pair of wallets; but transfers.from_wallet_id/to_wallet_id are
// foreign keys, so inserting the row also takes an implicit shared lock on
// both wallet rows. If that insert happened before our explicit FOR UPDATE,
// two concurrent transactions could each hold the weak FK lock and then
// deadlock trying to upgrade to the exclusive lock the other is holding.
// Taking the strong lock first avoids that: the FK check on our own insert
// is then satisfied by a lock this same transaction already holds.
//
// Idempotency strategy: the transfers table has a UNIQUE constraint on
// idempotency_key. A plain lookup short-circuits the common replay case
// before any locking; INSERT ... ON CONFLICT DO NOTHING then atomically
// claims the key for genuinely new requests, closing the race where two
// requests with the same brand-new key arrive at once. If the claim fails
// (row already exists), the existing transfer — whatever its current status
// — is returned unchanged and no side effects are re-applied.
func (s *TransferService) CreateTransfer(ctx context.Context, req domain.CreateTransferRequest) (*domain.Transfer, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	var result *domain.Transfer

	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		transferRepo := postgres.NewTransferRepo(tx)
		walletRepo := postgres.NewWalletRepo(tx)
		ledgerRepo := postgres.NewLedgerRepo(tx)

		if existing, err := transferRepo.GetByIdempotencyKey(ctx, req.IdempotencyKey); err == nil {
			result = existing
			return nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("check idempotency key: %w", err)
		}

		firstID, secondID := req.FromWalletID, req.ToWalletID
		if secondID < firstID {
			firstID, secondID = secondID, firstID
		}
		firstWallet, err := walletRepo.GetForUpdate(ctx, firstID)
		if err != nil {
			return err
		}
		secondWallet, err := walletRepo.GetForUpdate(ctx, secondID)
		if err != nil {
			return err
		}

		fromWallet, toWallet := firstWallet, secondWallet
		if fromWallet.ID != req.FromWalletID {
			fromWallet, toWallet = secondWallet, firstWallet
		}

		id := uuid.NewString()
		created, _, err := transferRepo.InsertPending(ctx, domain.Transfer{
			ID:             id,
			IdempotencyKey: req.IdempotencyKey,
			FromWalletID:   req.FromWalletID,
			ToWalletID:     req.ToWalletID,
			Amount:         req.Amount,
		})
		if err != nil {
			return fmt.Errorf("insert pending transfer: %w", err)
		}
		if !created {
			// Lost a race to a concurrent request with the same key. The
			// wallet locks acquired above are simply released on return.
			existing, err := transferRepo.GetByIdempotencyKey(ctx, req.IdempotencyKey)
			if err != nil {
				return fmt.Errorf("load existing transfer for idempotency key: %w", err)
			}
			result = existing
			return nil
		}

		if fromWallet.Balance < req.Amount {
			if err := transferRepo.MarkFailed(ctx, id, domain.ErrInsufficientFunds.Error()); err != nil {
				return fmt.Errorf("mark transfer failed: %w", err)
			}
			failed, err := transferRepo.GetByID(ctx, id)
			if err != nil {
				return err
			}
			result = failed
			return nil
		}

		if err := walletRepo.UpdateBalance(ctx, fromWallet.ID, fromWallet.Balance-req.Amount); err != nil {
			return fmt.Errorf("debit wallet: %w", err)
		}
		if err := walletRepo.UpdateBalance(ctx, toWallet.ID, toWallet.Balance+req.Amount); err != nil {
			return fmt.Errorf("credit wallet: %w", err)
		}

		entries := []domain.LedgerEntry{
			{ID: uuid.NewString(), TransferID: id, WalletID: fromWallet.ID, Type: domain.EntryDebit, Amount: req.Amount},
			{ID: uuid.NewString(), TransferID: id, WalletID: toWallet.ID, Type: domain.EntryCredit, Amount: req.Amount},
		}
		if err := ledgerRepo.InsertEntries(ctx, entries); err != nil {
			return fmt.Errorf("insert ledger entries: %w", err)
		}

		if err := transferRepo.MarkProcessed(ctx, id); err != nil {
			return fmt.Errorf("mark transfer processed: %w", err)
		}
		processed, err := transferRepo.GetByID(ctx, id)
		if err != nil {
			return err
		}
		result = processed
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *TransferService) GetWalletBalance(ctx context.Context, walletID string) (*domain.Wallet, error) {
	repo := postgres.NewWalletRepo(s.pool)
	return repo.Get(ctx, walletID)
}

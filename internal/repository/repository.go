package repository

import (
	"context"
	"errors"
	"fmt"

	db "github.com/chaitanya-bhagat/wallet-transfer-assignment/database/generated"
	"github.com/chaitanya-bhagat/wallet-transfer-assignment/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type transfer struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewTransferRepo(pool *pgxpool.Pool, queries *db.Queries) Repository {
	return &transfer{
		pool:    pool,
		queries: queries,
	}
}

func (t *transfer) CreateTransfer(ctx context.Context, input model.CreateTransferRequest) (model.Transfer, error) {
	transfer, err := t.queries.GetTransferByIdempotencyKey(ctx, input.IdempotencyKey)
	if err == nil {
		return model.MapToTransfer(transfer), fmt.Errorf("idempotency key conflict")
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return model.Transfer{}, fmt.Errorf("check idempotency key: %w", err)
	}
	tx, err := t.pool.Begin(ctx)
	if err != nil {
		return model.Transfer{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	qtx := t.queries.WithTx(tx)

	wallets, err := qtx.GetWalletsForUpdate(ctx, []string{input.FromWalletID, input.ToWalletID})
	if err != nil {
		return model.Transfer{}, fmt.Errorf("failed to get wallet")
	}
	if len(wallets) != 2 {
		return model.Transfer{}, model.ErrWalletNotFound
	}

	newTransfer, err := qtx.CreateTransfer(ctx,
		db.CreateTransferParams{
			ID:             uuid.NewString(),
			IdempotencyKey: input.IdempotencyKey,
			FromWalletID:   input.FromWalletID,
			ToWalletID:     input.ToWalletID,
			Amount:         input.Amount,
			Status:         input.Status,
		})
	if err != nil {
		if isUniqueViolation(err) {
			existingTransfer, err := t.queries.GetTransferByIdempotencyKey(ctx, input.IdempotencyKey)
			if err != nil {
				return model.Transfer{}, fmt.Errorf("load existing transfer")
			}
			return model.MapToTransfer(existingTransfer), nil
		}
		return model.Transfer{}, fmt.Errorf("transfer failed")
	}

	var source, dest db.Wallet

	for _, wallet := range wallets {
		switch wallet.ID {
		case input.FromWalletID:
			source = wallet
		case input.ToWalletID:
			dest = wallet
		}
	}

	if source.Balance < input.Amount {
		return model.Transfer{}, model.ErrInsufficientFunds
	}

	if err = qtx.SetWalletBalance(ctx, db.SetWalletBalanceParams{
		ID:      source.ID,
		Balance: source.Balance - input.Amount,
	}); err != nil {
		return model.Transfer{}, fmt.Errorf("failed to update debit wallet")
	}

	if err = qtx.SetWalletBalance(ctx, db.SetWalletBalanceParams{
		ID:      dest.ID,
		Balance: dest.Balance + input.Amount,
	}); err != nil {
		return model.Transfer{}, fmt.Errorf("failed to credit wallet")
	}

	if err := qtx.CreateLedgerEntry(ctx, db.CreateLedgerEntryParams{
		ID:         uuid.NewString(),
		TransferID: newTransfer.ID,
		WalletID:   source.ID,
		EntryType:  "DEBIT",
		Amount:     input.Amount,
	}); err != nil {
		return model.Transfer{}, fmt.Errorf("failed to create debit ledger")
	}

	if err := qtx.CreateLedgerEntry(ctx, db.CreateLedgerEntryParams{
		ID:         uuid.NewString(),
		TransferID: newTransfer.ID,
		WalletID:   dest.ID,
		EntryType:  "CREDIT",
		Amount:     input.Amount,
	}); err != nil {
		return model.Transfer{}, fmt.Errorf("failed to create credit ledger")
	}

	processed, err := qtx.UpdateTransferStatus(ctx, db.UpdateTransferStatusParams{
		ID:     newTransfer.ID,
		Status: "PROCESSED",
	})
	if err != nil {
		return model.Transfer{}, fmt.Errorf("failed to update transfer status")
	}
	if err := tx.Commit(ctx); err != nil {
		return model.Transfer{}, fmt.Errorf("failed to commit transaction")
	}

	return model.MapToTransfer(processed), nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}

	return pgErr.SQLState() == "23505"
}

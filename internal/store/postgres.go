package store

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/robustrade/wallet-transfer/internal/domain"
)

//go:embed schema.sql
var schema string

type Postgres struct{ pool *pgxpool.Pool }

func NewPostgres(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

func (p *Postgres) Migrate(ctx context.Context) error {
	_, err := p.pool.Exec(ctx, schema)
	return err
}

func (p *Postgres) GetWallet(ctx context.Context, id string) (domain.Wallet, error) {
	var wallet domain.Wallet
	err := p.pool.QueryRow(ctx, `SELECT id, balance FROM wallets WHERE id = $1`, id).Scan(&wallet.ID, &wallet.Balance)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Wallet{}, fmt.Errorf("wallet with id '%s' does not exist, err: %w", id, domain.ErrWalletNotFound)
	}
	return wallet, err
}

func (p *Postgres) CreateTransfer(ctx context.Context, request domain.TransferRequest) (domain.TransferResult, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return domain.TransferResult{}, err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `INSERT INTO idempotency_records
		(idempotency_key, from_wallet_id, to_wallet_id, amount)
		VALUES ($1, $2, $3, $4) ON CONFLICT (idempotency_key) DO NOTHING`,
		request.IdempotencyKey, request.FromWalletID, request.ToWalletID, request.Amount)
	if err != nil {
		return domain.TransferResult{}, err
	}

	var fromID, toID string
	var amount int64
	var transferID *uuid.UUID
	var httpStatus *int16
	err = tx.QueryRow(ctx, `SELECT from_wallet_id, to_wallet_id, amount, transfer_id, http_status
		FROM idempotency_records WHERE idempotency_key = $1 FOR UPDATE`, request.IdempotencyKey).
		Scan(&fromID, &toID, &amount, &transferID, &httpStatus)
	if err != nil {
		return domain.TransferResult{}, err
	}
	if fromID != request.FromWalletID || toID != request.ToWalletID || amount != request.Amount {
		return domain.TransferResult{}, domain.ErrIdempotencyConflict
	}
	if transferID != nil && httpStatus != nil {
		var result domain.TransferResult
		result.HTTPStatus = int(*httpStatus)
		err = tx.QueryRow(ctx, `SELECT id, from_wallet_id, to_wallet_id, amount, status FROM transfers WHERE id = $1`, *transferID).
			Scan(&result.Transfer.ID, &result.Transfer.FromWalletID, &result.Transfer.ToWalletID, &result.Transfer.Amount, &result.Transfer.Status)
		if err != nil {
			return domain.TransferResult{}, err
		}
		if err = tx.Commit(ctx); err != nil {
			return domain.TransferResult{}, err
		}
		return result, nil
	}

	walletIDs := []string{request.FromWalletID, request.ToWalletID}
	sort.Strings(walletIDs)
	for _, walletID := range walletIDs {
		var lockedID string
		err := tx.QueryRow(ctx, `SELECT id FROM wallets WHERE id = $1 FOR UPDATE`, walletID).Scan(&lockedID)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.TransferResult{}, fmt.Errorf("wallet with id '%s' does not exist, err: %w", walletID, domain.ErrWalletNotFound)
		}
		if err != nil {
			return domain.TransferResult{}, err
		}
	}

	id := uuid.New()
	transfer := domain.Transfer{ID: id.String(), FromWalletID: request.FromWalletID, ToWalletID: request.ToWalletID, Amount: request.Amount, Status: "PENDING"}
	_, err = tx.Exec(ctx, `INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status) VALUES ($1, $2, $3, $4, 'PENDING')`, id, request.FromWalletID, request.ToWalletID, request.Amount)
	if err != nil {
		return domain.TransferResult{}, err
	}

	var balance int64
	err = tx.QueryRow(ctx, `SELECT balance FROM wallets WHERE id = $1`, request.FromWalletID).Scan(&balance)
	if err != nil {
		return domain.TransferResult{}, err
	}
	if balance < request.Amount {
		transfer.Status = "FAILED"
		if _, err = tx.Exec(ctx, `UPDATE transfers SET status = 'FAILED' WHERE id = $1`, id); err != nil {
			return domain.TransferResult{}, err
		}
		if _, err = tx.Exec(ctx, `UPDATE idempotency_records SET transfer_id = $2, http_status = 422 WHERE idempotency_key = $1`, request.IdempotencyKey, id); err != nil {
			return domain.TransferResult{}, err
		}
		if err = tx.Commit(ctx); err != nil {
			return domain.TransferResult{}, err
		}
		return domain.TransferResult{Transfer: transfer, HTTPStatus: 422}, nil
	}

	if _, err = tx.Exec(ctx, `UPDATE wallets SET balance = balance - $2 WHERE id = $1`, request.FromWalletID, request.Amount); err != nil {
		return domain.TransferResult{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE wallets SET balance = balance + $2 WHERE id = $1`, request.ToWalletID, request.Amount); err != nil {
		return domain.TransferResult{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO ledger_entries (wallet_id, transfer_id, type, amount) VALUES ($1, $2, 'DEBIT', $3), ($4, $2, 'CREDIT', $3)`, request.FromWalletID, id, request.Amount, request.ToWalletID); err != nil {
		return domain.TransferResult{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE transfers SET status = 'PROCESSED' WHERE id = $1`, id); err != nil {
		return domain.TransferResult{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE idempotency_records SET transfer_id = $2, http_status = 201 WHERE idempotency_key = $1`, request.IdempotencyKey, id); err != nil {
		return domain.TransferResult{}, err
	}
	transfer.Status = "PROCESSED"
	if err = tx.Commit(ctx); err != nil {
		return domain.TransferResult{}, fmt.Errorf("commit transfer: %w", err)
	}
	return domain.TransferResult{Transfer: transfer, HTTPStatus: 201}, nil
}

package repository

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"github.com/ankit729/wallet-transfer/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrIdempotencyConflict = errors.New(
		"idempotency key was already used with a different request",
	)
	ErrWalletAlreadyExists = errors.New("wallet already exists")
	ErrWalletNotFound      = errors.New("wallet not found")
	ErrTransferNotFound    = errors.New("transfer not found")
)

const maxInt64 = int64(1<<63 - 1)

type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Ping(ctx context.Context) error {
	return r.pool.Ping(ctx)
}

func (r *Repository) CreateWallet(
	ctx context.Context,
	request domain.CreateWalletRequest,
) (domain.Wallet, error) {
	var wallet domain.Wallet

	err := r.pool.QueryRow(
		ctx,
		`
		INSERT INTO wallets (
			id,
			currency,
			balance
		)
		VALUES ($1, $2, $3)
		RETURNING id, currency, balance, created_at, updated_at
		`,
		request.ID,
		request.Currency,
		request.InitialBalance,
	).Scan(
		&wallet.ID,
		&wallet.Currency,
		&wallet.Balance,
		&wallet.CreatedAt,
		&wallet.UpdatedAt,
	)

	if err != nil {
		var pgError *pgconn.PgError
		if errors.As(err, &pgError) && pgError.Code == "23505" {
			return domain.Wallet{}, ErrWalletAlreadyExists
		}

		return domain.Wallet{}, err
	}

	return wallet, nil
}

func (r *Repository) GetWallet(
	ctx context.Context,
	walletID string,
) (domain.Wallet, error) {
	return scanWallet(r.pool.QueryRow(
		ctx,
		`
		SELECT id, currency, balance, created_at, updated_at
		FROM wallets
		WHERE id = $1
		`,
		walletID,
	))
}

func (r *Repository) CreateTransfer(
	ctx context.Context,
	request domain.TransferRequest,
	requestHash string,
) (domain.TransferResult, *domain.TransferEvent, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.TransferResult{}, nil, err
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	transferID := uuid.New()
	shardID := shardForKey(request.IdempotencyKey)

	err = tx.QueryRow(
		ctx,
		`
		INSERT INTO transfers (
			id,
			idempotency_key,
			shard_id,
			request_hash,
			from_wallet_id,
			to_wallet_id,
			amount,
			status,
			response_code,
			response_body
		)
		VALUES (
			$1,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			'PENDING',
			0,
			'{}'::jsonb
		)
		ON CONFLICT (idempotency_key, shard_id) DO NOTHING
		RETURNING id
		`,
		transferID,
		request.IdempotencyKey,
		shardID,
		requestHash,
		request.FromWalletID,
		request.ToWalletID,
		request.Amount,
	).Scan(&transferID)

	if errors.Is(err, pgx.ErrNoRows) {
		existing, lookupErr := scanTransfer(tx.QueryRow(
			ctx,
			transferSelectSQL+` WHERE idempotency_key = $1 AND shard_id = $2`,
			request.IdempotencyKey,
			shardID,
		))
		if lookupErr != nil {
			return domain.TransferResult{}, nil, lookupErr
		}

		if existing.RequestHash != requestHash {
			return domain.TransferResult{}, nil, ErrIdempotencyConflict
		}

		existing.Replayed = true
		return existing, nil, nil
	}

	if err != nil {
		return domain.TransferResult{}, nil, err
	}

	responseBody, err := json.Marshal(domain.TransferResponse{
		TransferID:   transferID.String(),
		Status:       string(domain.TransferPending),
		FromWalletID: request.FromWalletID,
		ToWalletID:   request.ToWalletID,
		Amount:       request.Amount,
	})
	if err != nil {
		return domain.TransferResult{}, nil, err
	}

	_, err = tx.Exec(
		ctx,
		`
		UPDATE transfers
		SET status = 'PENDING',
		    response_code = 202,
		    response_body = $2::jsonb
		WHERE id = $1
		  AND status = 'PENDING'
		`,
		transferID,
		responseBody,
	)
	if err != nil {
		return domain.TransferResult{}, nil, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO transfer_events (transfer_id, idempotency_key, shard_id, event_type)
		VALUES ($1, $2, $3, 'DEBIT_WALLET')
		ON CONFLICT (transfer_id, event_type) DO NOTHING
	`, transferID, request.IdempotencyKey, shardID)
	if err != nil {
		return domain.TransferResult{}, nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.TransferResult{}, nil, err
	}

	result, err := r.GetTransfer(ctx, transferID.String())
	if err != nil {
		return domain.TransferResult{}, nil, err
	}
	return result, &domain.TransferEvent{
		TransferID:     transferID.String(),
		IdempotencyKey: request.IdempotencyKey,
		ShardID:        shardID,
		Type:           domain.DebitWalletEvent,
	}, nil
}

func shardForKey(key string) int {
	sum := sha256.Sum256([]byte(key))
	return int(sum[0] % 16)
}

func (r *Repository) ProcessDebit(
	ctx context.Context,
	event domain.TransferEvent,
) (domain.TransferEvent, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.TransferEvent{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var request domain.TransferRequest
	var transferID uuid.UUID
	var status domain.TransferStatus
	err = tx.QueryRow(ctx, `
		SELECT id, status, idempotency_key, from_wallet_id, to_wallet_id, amount
		FROM transfers WHERE id = $1 AND shard_id = $2 FOR UPDATE
	`, event.TransferID, event.ShardID).Scan(
		&transferID, &status, &request.IdempotencyKey,
		&request.FromWalletID, &request.ToWalletID, &request.Amount,
	)
	if err != nil {
		return domain.TransferEvent{}, err
	}
	if status != domain.TransferPending {
		return domain.TransferEvent{}, tx.Commit(ctx)
	}

	var debitExists bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM ledger_entries
			WHERE transfer_id = $1 AND wallet_id = $2 AND entry_type = 'DEBIT'
		)
	`, transferID, request.FromWalletID).Scan(&debitExists)
	if err != nil {
		return domain.TransferEvent{}, err
	}
	if debitExists {
		if err := tx.Commit(ctx); err != nil {
			return domain.TransferEvent{}, err
		}
		return domain.TransferEvent{
			TransferID: transferID.String(), IdempotencyKey: request.IdempotencyKey,
			ShardID: event.ShardID, Type: domain.CreditWalletEvent,
		}, nil
	}

	wallet, err := lockWallet(ctx, tx, request.FromWalletID)
	if err != nil {
		return domain.TransferEvent{}, err
	}
	var destinationCurrency string
	if err := tx.QueryRow(ctx, `SELECT currency FROM wallets WHERE id = $1`,
		request.ToWalletID).Scan(&destinationCurrency); err != nil {
		return domain.TransferEvent{}, err
	}
	if wallet.Balance < request.Amount || wallet.Currency != destinationCurrency {
		code := "INSUFFICIENT_FUNDS"
		message := "source wallet has insufficient funds"
		if wallet.Currency != destinationCurrency {
			code = "DIFFERENT_CURRENCIES"
			message = "source and destination wallets must use the same currency"
		}
		if _, err := r.failTransfer(ctx, tx, transferID, code,
			message, 422); err != nil {
			return domain.TransferEvent{}, err
		}
		return domain.TransferEvent{}, nil
	}

	if _, err = tx.Exec(ctx, `
		UPDATE wallets SET balance = balance - $1, updated_at = NOW() WHERE id = $2
	`, request.Amount, request.FromWalletID); err != nil {
		return domain.TransferEvent{}, err
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO ledger_entries (wallet_id, transfer_id, entry_type, amount)
		VALUES ($1, $2, 'DEBIT', $3)
		ON CONFLICT (transfer_id, wallet_id, entry_type) DO NOTHING
	`, request.FromWalletID, transferID, request.Amount); err != nil {
		return domain.TransferEvent{}, err
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO transfer_events (transfer_id, idempotency_key, shard_id, event_type)
		VALUES ($1, $2, $3, 'CREDIT_WALLET')
		ON CONFLICT (transfer_id, event_type) DO NOTHING
	`, transferID, request.IdempotencyKey, event.ShardID); err != nil {
		return domain.TransferEvent{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.TransferEvent{}, err
	}

	return domain.TransferEvent{
		TransferID: transferID.String(), IdempotencyKey: request.IdempotencyKey,
		ShardID: event.ShardID, Type: domain.CreditWalletEvent,
	}, nil
}

func (r *Repository) ProcessCredit(
	ctx context.Context,
	event domain.TransferEvent,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var request domain.TransferRequest
	var transferID uuid.UUID
	var status domain.TransferStatus
	err = tx.QueryRow(ctx, `
		SELECT id, status, idempotency_key, from_wallet_id, to_wallet_id, amount
		FROM transfers WHERE id = $1 AND shard_id = $2 FOR UPDATE
	`, event.TransferID, event.ShardID).Scan(
		&transferID, &status, &request.IdempotencyKey,
		&request.FromWalletID, &request.ToWalletID, &request.Amount,
	)
	if err != nil {
		return err
	}
	if status != domain.TransferPending {
		return tx.Commit(ctx)
	}

	wallet, err := lockWallet(ctx, tx, request.ToWalletID)
	if err != nil {
		return err
	}
	if wallet.Balance > maxInt64-request.Amount {
		return r.compensateDebit(ctx, tx, transferID, request)
	}
	if _, err = tx.Exec(ctx, `
		UPDATE wallets SET balance = balance + $1, updated_at = NOW() WHERE id = $2
	`, request.Amount, request.ToWalletID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO ledger_entries (wallet_id, transfer_id, entry_type, amount)
		VALUES ($1, $2, 'CREDIT', $3)
		ON CONFLICT (transfer_id, wallet_id, entry_type) DO NOTHING
	`, request.ToWalletID, transferID, request.Amount); err != nil {
		return err
	}
	responseBody, err := json.Marshal(domain.TransferResponse{
		TransferID: transferID.String(), Status: string(domain.TransferProcessed),
		FromWalletID: request.FromWalletID, ToWalletID: request.ToWalletID, Amount: request.Amount,
	})
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `
		UPDATE transfers SET status = 'PROCESSED', response_code = 201,
		response_body = $2::jsonb, processed_at = NOW()
		WHERE id = $1 AND status = 'PENDING'
	`, transferID, responseBody); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) compensateDebit(
	ctx context.Context,
	tx pgx.Tx,
	transferID uuid.UUID,
	request domain.TransferRequest,
) error {
	if _, err := tx.Exec(ctx, `
		UPDATE wallets SET balance = balance + $1, updated_at = NOW() WHERE id = $2
	`, request.Amount, request.FromWalletID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO ledger_entries (wallet_id, transfer_id, entry_type, amount)
		VALUES ($1, $2, 'COMPENSATING_CREDIT', $3)
		ON CONFLICT (transfer_id, wallet_id, entry_type) DO NOTHING
	`, request.FromWalletID, transferID, request.Amount); err != nil {
		return err
	}
	_, err := r.failTransfer(ctx, tx, transferID, "BALANCE_OVERFLOW",
		"destination wallet balance would overflow", 422)
	return err
}

func (r *Repository) failTransfer(
	ctx context.Context,
	tx pgx.Tx,
	transferID uuid.UUID,
	code string,
	message string,
	responseCode int,
) (domain.TransferResult, error) {
	responseBody, err := json.Marshal(domain.ErrorResponse{
		Error: domain.APIError{
			Code:    code,
			Message: message,
		},
	})
	if err != nil {
		return domain.TransferResult{}, err
	}

	_, err = tx.Exec(
		ctx,
		`
		UPDATE transfers
		SET status = 'FAILED',
		    response_code = $2,
		    response_body = $3::jsonb,
		    failure_code = $4,
		    failure_message = $5
		WHERE id = $1
		  AND status = 'PENDING'
		`,
		transferID,
		responseCode,
		responseBody,
		code,
		message,
	)
	if err != nil {
		return domain.TransferResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.TransferResult{}, err
	}

	return r.GetTransfer(ctx, transferID.String())
}

func lockWallet(
	ctx context.Context,
	tx pgx.Tx,
	walletID string,
) (domain.Wallet, error) {
	return scanWallet(tx.QueryRow(
		ctx,
		`
		SELECT id, currency, balance, created_at, updated_at
		FROM wallets
		WHERE id = $1
		FOR UPDATE
		`,
		walletID,
	))
}

func scanWallet(row pgx.Row) (domain.Wallet, error) {
	var wallet domain.Wallet

	err := row.Scan(
		&wallet.ID,
		&wallet.Currency,
		&wallet.Balance,
		&wallet.CreatedAt,
		&wallet.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Wallet{}, ErrWalletNotFound
	}
	if err != nil {
		return domain.Wallet{}, err
	}

	return wallet, nil
}

const transferSelectSQL = `
	SELECT
		id::text,
		idempotency_key,
		request_hash,
		status::text,
		from_wallet_id,
		to_wallet_id,
		amount,
		created_at,
		processed_at,
		failure_code,
		failure_message,
		response_code,
		response_body
	FROM transfers
`

func (r *Repository) GetTransfer(
	ctx context.Context,
	transferID string,
) (domain.TransferResult, error) {
	if _, err := uuid.Parse(transferID); err != nil {
		return domain.TransferResult{}, ErrTransferNotFound
	}

	result, err := scanTransfer(r.pool.QueryRow(
		ctx,
		transferSelectSQL+` WHERE id = $1`,
		transferID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TransferResult{}, ErrTransferNotFound
	}

	return result, err
}

func scanTransfer(row pgx.Row) (domain.TransferResult, error) {
	var result domain.TransferResult
	var status string
	var responseBody []byte
	var failureCode *string
	var failureMessage *string

	err := row.Scan(
		&result.ID,
		&result.IdempotencyKey,
		&result.RequestHash,
		&status,
		&result.FromWalletID,
		&result.ToWalletID,
		&result.Amount,
		&result.CreatedAt,
		&result.ProcessedAt,
		&failureCode,
		&failureMessage,
		&result.ResponseCode,
		&responseBody,
	)
	if err != nil {
		return domain.TransferResult{}, err
	}

	result.Status = domain.TransferStatus(status)
	result.ResponseBody = append([]byte(nil), responseBody...)

	if failureCode != nil {
		result.FailureCode = *failureCode
	}

	if failureMessage != nil {
		result.FailureMessage = *failureMessage
	}

	return result, nil
}

func (r *Repository) CountLedgerEntries(
	ctx context.Context,
	transferID string,
) (int, error) {
	var count int

	err := r.pool.QueryRow(
		ctx,
		`
		SELECT COUNT(*)
		FROM ledger_entries
		WHERE transfer_id = $1
		`,
		transferID,
	).Scan(&count)

	return count, err
}

func (r *Repository) Pool() *pgxpool.Pool {
	return r.pool
}

func (r *Repository) String() string {
	return "postgres repository"
}

var _ = time.Time{}

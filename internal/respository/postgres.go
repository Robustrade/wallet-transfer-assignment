package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type txKey struct{}

type pgxConn interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// PostgresTransactionManager manages transaction scope for repository operations.
// The application/service layer should own transaction boundaries and pass the
// transaction-enabled context to repository methods.
type PostgresTransactionManager struct {
	pool *pgxpool.Pool
}

func NewPostgresTransactionManager(pool *pgxpool.Pool) *PostgresTransactionManager {
	return &PostgresTransactionManager{pool: pool}
}

func (m *PostgresTransactionManager) RunInTransaction(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	if ctx.Value(txKey{}) != nil {
		return errors.New("nested transactions are not supported")
	}

	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadWrite})
	if err != nil {
		return err
	}

	ctx = context.WithValue(ctx, txKey{}, tx)
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	if err = fn(ctx); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func getConn(ctx context.Context, pool *pgxpool.Pool) pgxConn {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return pool
}

var (
	ErrNotFound            = errors.New("record not found")
	ErrConflict            = errors.New("conflict")
	ErrTransactionRequired = errors.New("transaction required for FOR UPDATE")
)

func mapDBError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23505" {
			return ErrConflict
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func NewPostgresWalletRepository(pool *pgxpool.Pool) WalletRepository {
	return &PostgresWalletRepository{pool: pool}
}

func NewPostgresTransferRepository(pool *pgxpool.Pool) TransferRepository {
	return &PostgresTransferRepository{pool: pool}
}

func NewPostgresLedgerRepository(pool *pgxpool.Pool) LedgerRepository {
	return &PostgresLedgerRepository{pool: pool}
}

func NewPostgresIdempotencyRepository(pool *pgxpool.Pool) IdempotencyRepository {
	return &PostgresIdempotencyRepository{pool: pool}
}

type PostgresWalletRepository struct {
	pool *pgxpool.Pool
}

type PostgresTransferRepository struct {
	pool *pgxpool.Pool
}

type PostgresLedgerRepository struct {
	pool *pgxpool.Pool
}

type PostgresIdempotencyRepository struct {
	pool *pgxpool.Pool
}

func (r *PostgresWalletRepository) Create(ctx context.Context, wallet *domain.Wallet) error {
	_, err := getConn(ctx, r.pool).Exec(ctx,
		`INSERT INTO wallets (id, balance, version, created_at, updated_at)
		 VALUES ($1, $2, $3, NOW(), NOW())`,
		wallet.ID,
		wallet.Balance,
		wallet.Version,
	)
	return mapDBError(err)
}

func (r *PostgresWalletRepository) GetByID(ctx context.Context, walletID string) (*domain.Wallet, error) {
	var wallet domain.Wallet
	err := getConn(ctx, r.pool).QueryRow(ctx,
		`SELECT id, balance, version FROM wallets WHERE id = $1`,
		walletID,
	).Scan(&wallet.ID, &wallet.Balance, &wallet.Version)
	if err != nil {
		return nil, mapDBError(err)
	}
	return &wallet, nil
}

func (r *PostgresWalletRepository) GetByIDForUpdate(ctx context.Context, walletID string) (*domain.Wallet, error) {
	if ctx.Value(txKey{}) == nil {
		return nil, ErrTransactionRequired
	}
	var wallet domain.Wallet
	err := getConn(ctx, r.pool).QueryRow(ctx,
		`SELECT id, balance, version FROM wallets WHERE id = $1 FOR UPDATE`,
		walletID,
	).Scan(&wallet.ID, &wallet.Balance, &wallet.Version)
	if err != nil {
		return nil, mapDBError(err)
	}
	return &wallet, nil
}

func (r *PostgresWalletRepository) Update(ctx context.Context, wallet *domain.Wallet) error {
	var newVersion int64
	row := getConn(ctx, r.pool).QueryRow(ctx,
		`UPDATE wallets
		 SET balance = $1,
		     version = version + 1,
		     updated_at = NOW()
		 WHERE id = $2
		 RETURNING version`,
		wallet.Balance,
		wallet.ID,
	)
	if err := row.Scan(&newVersion); err != nil {
		return mapDBError(err)
	}
	wallet.Version = newVersion
	return nil
}

func (r *PostgresTransferRepository) Create(ctx context.Context, transfer *domain.Transfer) error {
	_, err := getConn(ctx, r.pool).Exec(ctx,
		`INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())`,
		transfer.ID,
		transfer.IdempotencyKey,
		transfer.FromWalletID,
		transfer.ToWalletID,
		transfer.Amount,
		string(transfer.Status),
		transfer.FailureReason,
	)
	return mapDBError(err)
}

func (r *PostgresTransferRepository) GetByID(ctx context.Context, transferID string) (*domain.Transfer, error) {
	var transfer domain.Transfer
	err := getConn(ctx, r.pool).QueryRow(ctx,
		`SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason
		 FROM transfers
		 WHERE id = $1`,
		transferID,
	).Scan(
		&transfer.ID,
		&transfer.IdempotencyKey,
		&transfer.FromWalletID,
		&transfer.ToWalletID,
		&transfer.Amount,
		&transfer.Status,
		&transfer.FailureReason,
	)
	if err != nil {
		return nil, mapDBError(err)
	}
	return &transfer, nil
}

func (r *PostgresTransferRepository) GetByIdempotencyKey(ctx context.Context, idempotencyKey string) (*domain.Transfer, error) {
	var transfer domain.Transfer
	err := getConn(ctx, r.pool).QueryRow(ctx,
		`SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason
		 FROM transfers
		 WHERE idempotency_key = $1`,
		idempotencyKey,
	).Scan(
		&transfer.ID,
		&transfer.IdempotencyKey,
		&transfer.FromWalletID,
		&transfer.ToWalletID,
		&transfer.Amount,
		&transfer.Status,
		&transfer.FailureReason,
	)
	if err != nil {
		return nil, mapDBError(err)
	}
	return &transfer, nil
}

func (r *PostgresTransferRepository) UpdateStatus(ctx context.Context, transferID string, status domain.TransferStatus, failureReason string) error {
	if !status.IsValid() {
		return ErrConflict
	}
	ct, err := getConn(ctx, r.pool).Exec(ctx,
		`UPDATE transfers
		 SET status = $1,
		     failure_reason = $2,
		     updated_at = NOW()
		 WHERE id = $3`,
		string(status),
		failureReason,
		transferID,
	)
	if err != nil {
		return mapDBError(err)
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresLedgerRepository) CreateEntries(ctx context.Context, entries []domain.LedgerEntry) error {
	if len(entries) != 2 {
		return errors.New("ledger entries must contain exactly two entries")
	}
	_, err := getConn(ctx, r.pool).Exec(ctx,
		`INSERT INTO ledger_entries (id, transfer_id, wallet_id, entry_type, amount, created_at)
		 VALUES ($1, $2, $3, $4, $5, NOW()),
		        ($6, $7, $8, $9, $10, NOW())`,
		entries[0].ID,
		entries[0].TransferID,
		entries[0].WalletID,
		string(entries[0].EntryType),
		entries[0].Amount,
		entries[1].ID,
		entries[1].TransferID,
		entries[1].WalletID,
		string(entries[1].EntryType),
		entries[1].Amount,
	)
	return mapDBError(err)
}

func (r *PostgresLedgerRepository) GetByTransferID(ctx context.Context, transferID string) ([]domain.LedgerEntry, error) {
	rows, err := getConn(ctx, r.pool).Query(ctx,
		`SELECT id, transfer_id, wallet_id, entry_type, amount
		 FROM ledger_entries
		 WHERE transfer_id = $1
		 ORDER BY entry_type`,
		transferID,
	)
	if err != nil {
		return nil, mapDBError(err)
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var entry domain.LedgerEntry
		var entryType string
		if err := rows.Scan(&entry.ID, &entry.TransferID, &entry.WalletID, &entryType, &entry.Amount); err != nil {
			return nil, err
		}
		entry.EntryType = domain.LedgerEntryType(entryType)
		entries = append(entries, entry)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return entries, nil
}

func (r *PostgresLedgerRepository) GetByWalletID(ctx context.Context, walletID string) ([]domain.LedgerEntry, error) {
	rows, err := getConn(ctx, r.pool).Query(ctx,
		`SELECT id, transfer_id, wallet_id, entry_type, amount
		 FROM ledger_entries
		 WHERE wallet_id = $1
		 ORDER BY created_at, id`,
		walletID,
	)
	if err != nil {
		return nil, mapDBError(err)
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var entry domain.LedgerEntry
		var entryType string
		if err := rows.Scan(&entry.ID, &entry.TransferID, &entry.WalletID, &entryType, &entry.Amount); err != nil {
			return nil, err
		}
		entry.EntryType = domain.LedgerEntryType(entryType)
		entries = append(entries, entry)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return entries, nil
}

func (r *PostgresIdempotencyRepository) Create(ctx context.Context, record *IdempotencyRecord) error {
	var transferID interface{}
	if record.TransferID == "" {
		transferID = nil
	} else {
		transferID = record.TransferID
	}
	_, err := getConn(ctx, r.pool).Exec(ctx,
		`INSERT INTO idempotency_records (idempotency_key, transfer_id, request_hash, status, response_json, status_code, created_at, created_by, caller_ip, user_agent)
		 VALUES ($1, $2, $3, $4, $5, $6, NOW(), $7, $8, $9)`,
		record.IdempotencyKey,
		transferID,
		record.RequestHash,
		string(record.Status),
		record.ResponseJSON,
		record.StatusCode,
		record.CreatedBy,
		record.CallerIP,
		record.UserAgent,
	)
	return mapDBError(err)
}

func (r *PostgresIdempotencyRepository) GetByKey(ctx context.Context, idempotencyKey string) (*IdempotencyRecord, error) {
	var record IdempotencyRecord
	err := getConn(ctx, r.pool).QueryRow(ctx,
		`SELECT idempotency_key, transfer_id, request_hash, status, response_json, status_code, created_at, created_by, caller_ip, user_agent
		 FROM idempotency_records
		 WHERE idempotency_key = $1`,
		idempotencyKey,
	).Scan(
		&record.IdempotencyKey,
		&record.TransferID,
		&record.RequestHash,
		&record.Status,
		&record.ResponseJSON,
		&record.StatusCode,
		&record.CreatedAt,
		&record.CreatedBy,
		&record.CallerIP,
		&record.UserAgent,
	)
	if err != nil {
		return nil, mapDBError(err)
	}
	return &record, nil
}

func (r *PostgresIdempotencyRepository) UpdateResponse(ctx context.Context, idempotencyKey string, responseJSON []byte, statusCode int, status IdempotencyStatus) error {
	ct, err := getConn(ctx, r.pool).Exec(ctx,
		`UPDATE idempotency_records
		 SET status = $1,
		     response_json = $2,
		     status_code = $3
		 WHERE idempotency_key = $4`,
		string(status),
		responseJSON,
		statusCode,
		idempotencyKey,
	)
	if err != nil {
		return mapDBError(err)
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresIdempotencyRepository) UpdateStatus(ctx context.Context, idempotencyKey string, status IdempotencyStatus) error {
	ct, err := getConn(ctx, r.pool).Exec(ctx,
		`UPDATE idempotency_records
		 SET status = $1
		 WHERE idempotency_key = $2`,
		string(status),
		idempotencyKey,
	)
	if err != nil {
		return mapDBError(err)
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresIdempotencyRepository) CleanupPending(ctx context.Context, cutoff time.Time) (int, error) {
	ct, err := getConn(ctx, r.pool).Exec(ctx,
		`UPDATE idempotency_records
		 SET status = $1, response_json = jsonb_build_object('error', 'stale idempotency claim'), status_code = 500
		 WHERE status = $2 AND created_at < $3`,
		string(IdempotencyStatusFailed),
		string(IdempotencyStatusPending),
		cutoff,
	)
	if err != nil {
		return 0, mapDBError(err)
	}
	return int(ct.RowsAffected()), nil
}

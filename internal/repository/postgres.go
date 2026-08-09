package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/shanks/wallet-transfer-assignment/internal/domain"
)

// DBExecutor abstracts database execution between *sql.DB and *sql.Tx.
type DBExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// PostgresTxManager executes functions within database transaction boundaries.
type PostgresTxManager struct {
	db *sql.DB
}

// NewPostgresTxManager constructs a new PostgresTxManager instance.
func NewPostgresTxManager(db *sql.DB) *PostgresTxManager {
	return &PostgresTxManager{db: db}
}

// ExecTx executes the given callback function inside an atomic SQL transaction.
func (m *PostgresTxManager) ExecTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := m.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			return fmt.Errorf("transaction error: %v, rollback error: %w", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// PostgresWalletRepository implements WalletRepository for PostgreSQL.
type PostgresWalletRepository struct {
	db *sql.DB
}

// NewPostgresWalletRepository constructs a PostgresWalletRepository.
func NewPostgresWalletRepository(db *sql.DB) *PostgresWalletRepository {
	return &PostgresWalletRepository{db: db}
}

func (r *PostgresWalletRepository) getExecutor(tx *sql.Tx) DBExecutor {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *PostgresWalletRepository) Create(ctx context.Context, tx *sql.Tx, wallet *domain.Wallet) error {
	exec := r.getExecutor(tx)
	query := `
		INSERT INTO wallets (id, balance, currency, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)
	`
	now := time.Now().UTC()
	if wallet.CreatedAt.IsZero() {
		wallet.CreatedAt = now
	}
	if wallet.UpdatedAt.IsZero() {
		wallet.UpdatedAt = now
	}

	_, err := exec.ExecContext(ctx, query, wallet.ID, wallet.Balance, wallet.Currency, wallet.CreatedAt, wallet.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to insert wallet: %w", err)
	}
	return nil
}

func (r *PostgresWalletRepository) GetByID(ctx context.Context, tx *sql.Tx, id string) (*domain.Wallet, error) {
	exec := r.getExecutor(tx)
	query := `
		SELECT id, balance, currency, created_at, updated_at
		FROM wallets
		WHERE id = $1
	`
	row := exec.QueryRowContext(ctx, query, id)
	var w domain.Wallet
	err := row.Scan(&w.ID, &w.Balance, &w.Currency, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, fmt.Errorf("failed to query wallet by ID: %w", err)
	}
	return &w, nil
}

func (r *PostgresWalletRepository) GetByIDForUpdate(ctx context.Context, tx *sql.Tx, id string) (*domain.Wallet, error) {
	exec := r.getExecutor(tx)
	query := `
		SELECT id, balance, currency, created_at, updated_at
		FROM wallets
		WHERE id = $1
		FOR UPDATE
	`
	row := exec.QueryRowContext(ctx, query, id)
	var w domain.Wallet
	err := row.Scan(&w.ID, &w.Balance, &w.Currency, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, fmt.Errorf("failed to lock wallet for update: %w", err)
	}
	return &w, nil
}

func (r *PostgresWalletRepository) GetTwoWalletsForUpdate(ctx context.Context, tx *sql.Tx, id1, id2 string) (*domain.Wallet, *domain.Wallet, error) {
	firstID, secondID := id1, id2
	if firstID > secondID {
		firstID, secondID = id2, id1
	}

	exec := r.getExecutor(tx)
	query := `
		SELECT id, balance, currency, created_at, updated_at
		FROM wallets
		WHERE id IN ($1, $2)
		ORDER BY id ASC
		FOR UPDATE
	`
	rows, err := exec.QueryContext(ctx, query, firstID, secondID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to lock wallets for update: %w", err)
	}
	defer rows.Close()

	var w1, w2 domain.Wallet
	count := 0
	for rows.Next() {
		var w domain.Wallet
		if err := rows.Scan(&w.ID, &w.Balance, &w.Currency, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, nil, fmt.Errorf("failed to scan wallet: %w", err)
		}
		if count == 0 {
			w1 = w
		} else {
			w2 = w
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if count < 2 {
		return nil, nil, domain.ErrWalletNotFound
	}

	var firstWallet, secondWallet *domain.Wallet
	if w1.ID == id1 {
		firstWallet = &w1
	} else if w2.ID == id1 {
		firstWallet = &w2
	}

	if w1.ID == id2 {
		secondWallet = &w1
	} else if w2.ID == id2 {
		secondWallet = &w2
	}

	if firstWallet == nil || secondWallet == nil {
		return nil, nil, domain.ErrWalletNotFound
	}

	return firstWallet, secondWallet, nil
}

func (r *PostgresWalletRepository) UpdateBalance(ctx context.Context, tx *sql.Tx, id string, newBalance int64) error {
	exec := r.getExecutor(tx)
	query := `
		UPDATE wallets
		SET balance = $1, updated_at = $2
		WHERE id = $3
	`
	res, err := exec.ExecContext(ctx, query, newBalance, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("failed to update wallet balance: %w", err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return domain.ErrWalletNotFound
	}
	return nil
}

func (r *PostgresWalletRepository) CalculateLedgerBalance(ctx context.Context, tx *sql.Tx, walletID string) (int64, error) {
	exec := r.getExecutor(tx)
	query := `
		SELECT COALESCE(SUM(
			CASE 
				WHEN entry_type = 'CREDIT' THEN amount
				WHEN entry_type = 'DEBIT' THEN -amount
				ELSE 0
			END
		), 0)
		FROM ledger_entries
		WHERE wallet_id = $1
	`
	var balance int64
	err := exec.QueryRowContext(ctx, query, walletID).Scan(&balance)
	if err != nil {
		return 0, fmt.Errorf("failed to calculate ledger balance: %w", err)
	}
	return balance, nil
}

// PostgresTransferRepository implements TransferRepository for PostgreSQL.
type PostgresTransferRepository struct {
	db *sql.DB
}

// NewPostgresTransferRepository constructs a PostgresTransferRepository.
func NewPostgresTransferRepository(db *sql.DB) *PostgresTransferRepository {
	return &PostgresTransferRepository{db: db}
}

func (r *PostgresTransferRepository) getExecutor(tx *sql.Tx) DBExecutor {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *PostgresTransferRepository) Create(ctx context.Context, tx *sql.Tx, transfer *domain.Transfer) error {
	exec := r.getExecutor(tx)
	query := `
		INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	now := time.Now().UTC()
	if transfer.CreatedAt.IsZero() {
		transfer.CreatedAt = now
	}
	if transfer.UpdatedAt.IsZero() {
		transfer.UpdatedAt = now
	}

	var failReason sql.NullString
	if transfer.FailureReason != "" {
		failReason = sql.NullString{String: transfer.FailureReason, Valid: true}
	}

	_, err := exec.ExecContext(ctx, query,
		transfer.ID,
		transfer.IdempotencyKey,
		transfer.FromWalletID,
		transfer.ToWalletID,
		transfer.Amount,
		string(transfer.Status),
		failReason,
		transfer.CreatedAt,
		transfer.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert transfer: %w", err)
	}
	return nil
}

func (r *PostgresTransferRepository) GetByID(ctx context.Context, tx *sql.Tx, id string) (*domain.Transfer, error) {
	exec := r.getExecutor(tx)
	query := `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
		FROM transfers
		WHERE id = $1
	`
	row := exec.QueryRowContext(ctx, query, id)
	var t domain.Transfer
	var statusStr string
	var failReason sql.NullString

	err := row.Scan(
		&t.ID,
		&t.IdempotencyKey,
		&t.FromWalletID,
		&t.ToWalletID,
		&t.Amount,
		&statusStr,
		&failReason,
		&t.CreatedAt,
		&t.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrTransferNotFound
		}
		return nil, fmt.Errorf("failed to query transfer by ID: %w", err)
	}

	t.Status = domain.TransferStatus(statusStr)
	if failReason.Valid {
		t.FailureReason = failReason.String
	}
	return &t, nil
}

func (r *PostgresTransferRepository) GetByIdempotencyKey(ctx context.Context, tx *sql.Tx, key string) (*domain.Transfer, error) {
	exec := r.getExecutor(tx)
	query := `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
		FROM transfers
		WHERE idempotency_key = $1
	`
	row := exec.QueryRowContext(ctx, query, key)
	var t domain.Transfer
	var statusStr string
	var failReason sql.NullString

	err := row.Scan(
		&t.ID,
		&t.IdempotencyKey,
		&t.FromWalletID,
		&t.ToWalletID,
		&t.Amount,
		&statusStr,
		&failReason,
		&t.CreatedAt,
		&t.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrTransferNotFound
		}
		return nil, fmt.Errorf("failed to query transfer by idempotency key: %w", err)
	}

	t.Status = domain.TransferStatus(statusStr)
	if failReason.Valid {
		t.FailureReason = failReason.String
	}
	return &t, nil
}

func (r *PostgresTransferRepository) UpdateStatus(ctx context.Context, tx *sql.Tx, id string, status domain.TransferStatus, failureReason string) error {
	exec := r.getExecutor(tx)
	query := `
		UPDATE transfers
		SET status = $1, failure_reason = $2, updated_at = $3
		WHERE id = $4
	`
	var failReason sql.NullString
	if failureReason != "" {
		failReason = sql.NullString{String: failureReason, Valid: true}
	}

	res, err := exec.ExecContext(ctx, query, string(status), failReason, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("failed to update transfer status: %w", err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return domain.ErrTransferNotFound
	}
	return nil
}

// PostgresLedgerRepository implements LedgerRepository for PostgreSQL.
type PostgresLedgerRepository struct {
	db *sql.DB
}

// NewPostgresLedgerRepository constructs a PostgresLedgerRepository.
func NewPostgresLedgerRepository(db *sql.DB) *PostgresLedgerRepository {
	return &PostgresLedgerRepository{db: db}
}

func (r *PostgresLedgerRepository) getExecutor(tx *sql.Tx) DBExecutor {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *PostgresLedgerRepository) CreateEntries(ctx context.Context, tx *sql.Tx, entries []domain.LedgerEntry) error {
	if len(entries) == 0 {
		return nil
	}
	exec := r.getExecutor(tx)
	query := `
		INSERT INTO ledger_entries (id, transfer_id, wallet_id, entry_type, amount, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	now := time.Now().UTC()
	for i := range entries {
		e := &entries[i]
		if e.CreatedAt.IsZero() {
			e.CreatedAt = now
		}
		_, err := exec.ExecContext(ctx, query, e.ID, e.TransferID, e.WalletID, string(e.Type), e.Amount, e.CreatedAt)
		if err != nil {
			return fmt.Errorf("failed to insert ledger entry: %w", err)
		}
	}
	return nil
}

func (r *PostgresLedgerRepository) GetByTransferID(ctx context.Context, tx *sql.Tx, transferID string) ([]domain.LedgerEntry, error) {
	exec := r.getExecutor(tx)
	query := `
		SELECT id, transfer_id, wallet_id, entry_type, amount, created_at
		FROM ledger_entries
		WHERE transfer_id = $1
		ORDER BY created_at ASC, id ASC
	`
	rows, err := exec.QueryContext(ctx, query, transferID)
	if err != nil {
		return nil, fmt.Errorf("failed to query ledger entries by transfer ID: %w", err)
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		var typeStr string
		if err := rows.Scan(&e.ID, &e.TransferID, &e.WalletID, &typeStr, &e.Amount, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan ledger entry: %w", err)
		}
		e.Type = domain.LedgerEntryType(typeStr)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error in GetByTransferID: %w", err)
	}
	return entries, nil
}

func (r *PostgresLedgerRepository) GetByWalletID(ctx context.Context, tx *sql.Tx, walletID string) ([]domain.LedgerEntry, error) {
	exec := r.getExecutor(tx)
	query := `
		SELECT id, transfer_id, wallet_id, entry_type, amount, created_at
		FROM ledger_entries
		WHERE wallet_id = $1
		ORDER BY created_at ASC, id ASC
	`
	rows, err := exec.QueryContext(ctx, query, walletID)
	if err != nil {
		return nil, fmt.Errorf("failed to query ledger entries by wallet ID: %w", err)
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		var typeStr string
		if err := rows.Scan(&e.ID, &e.TransferID, &e.WalletID, &typeStr, &e.Amount, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan ledger entry: %w", err)
		}
		e.Type = domain.LedgerEntryType(typeStr)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error in GetByWalletID: %w", err)
	}
	return entries, nil
}

// PostgresIdempotencyRepository implements IdempotencyRepository for PostgreSQL.
type PostgresIdempotencyRepository struct {
	db *sql.DB
}

// NewPostgresIdempotencyRepository constructs a PostgresIdempotencyRepository.
func NewPostgresIdempotencyRepository(db *sql.DB) *PostgresIdempotencyRepository {
	return &PostgresIdempotencyRepository{db: db}
}

func (r *PostgresIdempotencyRepository) getExecutor(tx *sql.Tx) DBExecutor {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *PostgresIdempotencyRepository) Create(ctx context.Context, tx *sql.Tx, rec *domain.IdempotencyRecord) error {
	exec := r.getExecutor(tx)
	query := `
		INSERT INTO idempotency_records (idempotency_key, request_hash, status, response_code, response_body, transfer_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	now := time.Now().UTC()
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	if rec.UpdatedAt.IsZero() {
		rec.UpdatedAt = now
	}

	var code sql.NullInt32
	if rec.ResponseCode != 0 {
		code = sql.NullInt32{Int32: int32(rec.ResponseCode), Valid: true}
	}
	var body sql.NullString
	if rec.ResponseBody != "" {
		body = sql.NullString{String: rec.ResponseBody, Valid: true}
	}
	var transferID sql.NullString
	if rec.TransferID != "" {
		transferID = sql.NullString{String: rec.TransferID, Valid: true}
	}

	_, err := exec.ExecContext(ctx, query,
		rec.IdempotencyKey,
		rec.RequestHash,
		string(rec.Status),
		code,
		body,
		transferID,
		rec.CreatedAt,
		rec.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert idempotency record: %w", err)
	}
	return nil
}

func (r *PostgresIdempotencyRepository) Get(ctx context.Context, tx *sql.Tx, key string) (*domain.IdempotencyRecord, error) {
	exec := r.getExecutor(tx)
	query := `
		SELECT idempotency_key, request_hash, status, response_code, response_body, transfer_id, created_at, updated_at
		FROM idempotency_records
		WHERE idempotency_key = $1
	`
	row := exec.QueryRowContext(ctx, query, key)
	return scanIdempotencyRecord(row)
}

func (r *PostgresIdempotencyRepository) GetForUpdate(ctx context.Context, tx *sql.Tx, key string) (*domain.IdempotencyRecord, error) {
	exec := r.getExecutor(tx)
	query := `
		SELECT idempotency_key, request_hash, status, response_code, response_body, transfer_id, created_at, updated_at
		FROM idempotency_records
		WHERE idempotency_key = $1
		FOR UPDATE
	`
	row := exec.QueryRowContext(ctx, query, key)
	return scanIdempotencyRecord(row)
}

func (r *PostgresIdempotencyRepository) Update(ctx context.Context, tx *sql.Tx, rec *domain.IdempotencyRecord) error {
	exec := r.getExecutor(tx)
	query := `
		UPDATE idempotency_records
		SET status = $1, response_code = $2, response_body = $3, transfer_id = $4, updated_at = $5
		WHERE idempotency_key = $6
	`
	var code sql.NullInt32
	if rec.ResponseCode != 0 {
		code = sql.NullInt32{Int32: int32(rec.ResponseCode), Valid: true}
	}
	var body sql.NullString
	if rec.ResponseBody != "" {
		body = sql.NullString{String: rec.ResponseBody, Valid: true}
	}
	var transferID sql.NullString
	if rec.TransferID != "" {
		transferID = sql.NullString{String: rec.TransferID, Valid: true}
	}

	_, err := exec.ExecContext(ctx, query,
		string(rec.Status),
		code,
		body,
		transferID,
		time.Now().UTC(),
		rec.IdempotencyKey,
	)
	if err != nil {
		return fmt.Errorf("failed to update idempotency record: %w", err)
	}
	return nil
}

func scanIdempotencyRecord(row *sql.Row) (*domain.IdempotencyRecord, error) {
	var rec domain.IdempotencyRecord
	var statusStr string
	var code sql.NullInt32
	var body sql.NullString
	var transferID sql.NullString

	err := row.Scan(
		&rec.IdempotencyKey,
		&rec.RequestHash,
		&statusStr,
		&code,
		&body,
		&transferID,
		&rec.CreatedAt,
		&rec.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil // Return nil record when not found
		}
		return nil, fmt.Errorf("failed to scan idempotency record: %w", err)
	}

	rec.Status = domain.IdempotencyStatus(statusStr)
	if code.Valid {
		rec.ResponseCode = int(code.Int32)
	}
	if body.Valid {
		rec.ResponseBody = body.String
	}
	if transferID.Valid {
		rec.TransferID = transferID.String
	}
	return &rec, nil
}

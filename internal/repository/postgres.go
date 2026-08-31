package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mshabab1296/wallet-transfer-assignment/internal/domain"
)

// migrationFiles is discovered at build time; add a numbered .sql file to the
// migrations directory and it will be applied automatically in filename order.
//
//go:embed migrations/*.sql
var migrationFiles embed.FS

// PostgresRepository owns PostgreSQL persistence and transaction boundaries.
type PostgresRepository struct{ db *sql.DB }

const maxTransactionAttempts = 3

// OpenPostgres verifies the connection and applies every embedded migration once.
func OpenPostgres(ctx context.Context, databaseURL string) (*PostgresRepository, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(4)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	r := &PostgresRepository{db: db}
	if err := r.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return r, nil
}

// Close releases the repository's database connections.
func (r *PostgresRepository) Close() error { return r.db.Close() }

// migrate records each successfully applied embedded migration in schema_migrations.
func (r *PostgresRepository) migrate(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`); err != nil {
		return err
	}
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		contents, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		migration := struct{ version, sql string }{
			version: strings.TrimSuffix(entry.Name(), ".sql"),
			sql:     string(contents),
		}
		var applied bool
		if err := r.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)", migration.version).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		tx, err := r.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		for _, statement := range strings.Split(migration.sql, "-- statement-break") {
			if strings.TrimSpace(statement) == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("apply migration %s: %w", migration.version, err)
			}
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(version) VALUES($1) ON CONFLICT DO NOTHING", migration.version); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// CreateWallet inserts a wallet with a non-negative opening balance.
func (r *PostgresRepository) CreateWallet(ctx context.Context, id string, balance int64) error {
	if id == "" || balance < 0 {
		return fmt.Errorf("wallet id and non-negative balance are required")
	}
	walletID, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("wallet id must be a UUID: %w", err)
	}
	_, err = r.db.ExecContext(ctx, "INSERT INTO wallets(id, balance) VALUES($1, $2)", walletID, balance)
	return err
}

// Balance returns a wallet's persisted balance.
func (r *PostgresRepository) Balance(ctx context.Context, id string) (int64, error) {
	var balance int64
	err := r.db.QueryRowContext(ctx, "SELECT balance FROM wallets WHERE id=$1", id).Scan(&balance)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, domain.ErrWalletNotFound
	}
	return balance, err
}

// TransferHistory returns recent transfers where the wallet was either party.
func (r *PostgresRepository) TransferHistory(ctx context.Context, walletID string, limit int) ([]domain.Transfer, error) {
	// Stage 1: distinguish an unknown wallet from a wallet with no transfer history.
	if _, err := r.Balance(ctx, walletID); err != nil {
		return nil, err
	}

	// Stage 2: read the most recent source or destination transfers using indexed columns.
	rows, err := r.db.QueryContext(ctx, `SELECT id,idempotency_key,from_wallet_id,to_wallet_id,amount,status,COALESCE(failure_reason,'') FROM transfers WHERE from_wallet_id=$1 OR to_wallet_id=$1 ORDER BY created_at DESC LIMIT $2`, walletID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	transfers := make([]domain.Transfer, 0)
	for rows.Next() {
		var transfer domain.Transfer
		if err := rows.Scan(&transfer.ID, &transfer.IdempotencyKey, &transfer.FromWalletID, &transfer.ToWalletID, &transfer.Amount, &transfer.Status, &transfer.FailureReason); err != nil {
			return nil, err
		}
		transfers = append(transfers, transfer)
	}
	return transfers, rows.Err()
}

// CreateTransfer atomically records an idempotent transfer and its ledger pair.
func (r *PostgresRepository) CreateTransfer(ctx context.Context, req domain.CreateTransferRequest) (domain.Transfer, error) {
	return retryTransaction(ctx, func(attemptContext context.Context) (domain.Transfer, error) {
		return r.createTransferOnce(attemptContext, req)
	})
}

// retryTransaction retries only transient PostgreSQL failures while preserving cancellation.
func retryTransaction(ctx context.Context, attemptFn func(context.Context) (domain.Transfer, error)) (domain.Transfer, error) {
	// Stage 1: retry only transient database conflicts; idempotency makes every attempt safe.
	for attempt := 1; attempt <= maxTransactionAttempts; attempt++ {
		transfer, err := attemptFn(ctx)
		if !isRetryableTransaction(err) || attempt == maxTransactionAttempts {
			return transfer, err
		}
		// Stage 2: wait briefly, while still respecting request cancellation, before retrying.
		backoff := time.NewTimer(time.Duration(attempt*10) * time.Millisecond)
		select {
		case <-ctx.Done():
			backoff.Stop()
			return domain.Transfer{}, ctx.Err()
		case <-backoff.C:
		}
	}
	return domain.Transfer{}, fmt.Errorf("transfer retry loop exhausted")
}

// createTransferOnce performs one complete transaction attempt.
func (r *PostgresRepository) createTransferOnce(ctx context.Context, req domain.CreateTransferRequest) (domain.Transfer, error) {
	// Stage 1: build the canonical request fingerprint used for idempotency checks.
	fingerprint := requestFingerprint(req)

	// Stage 2: begin the transaction that owns every transfer side effect.
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return domain.Transfer{}, err
	}
	defer tx.Rollback()

	// Stage 3: return an already-completed transfer for an identical request.
	if existing, found, err := getByKey(ctx, tx, req.IdempotencyKey); err != nil {
		return domain.Transfer{}, err
	} else if found {
		if existing.fingerprint != fingerprint {
			return domain.Transfer{}, domain.ErrIdempotencyConflict
		}
		if err := tx.Commit(); err != nil {
			return domain.Transfer{}, err
		}
		existing.transfer.Replayed = true
		if existing.transfer.Status == domain.Failed {
			return existing.transfer, domain.ErrInsufficientFunds
		}
		return existing.transfer, nil
	}

	// Stage 4: create the pending transfer and reserve its idempotency key.
	id, err := newTransferID()
	if err != nil {
		return domain.Transfer{}, err
	}
	now := time.Now().UTC()
	transfer := domain.Transfer{ID: id, IdempotencyKey: req.IdempotencyKey, FromWalletID: req.FromWalletID, ToWalletID: req.ToWalletID, Amount: req.Amount, Status: domain.Pending}
	if _, err := tx.ExecContext(ctx, `INSERT INTO transfers(id,idempotency_key,from_wallet_id,to_wallet_id,amount,status,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, req.IdempotencyKey, req.FromWalletID, req.ToWalletID, req.Amount, domain.Pending, now); err != nil {
		if isUniqueViolation(err) {
			return r.replayAfterDuplicate(ctx, req.IdempotencyKey, fingerprint)
		}
		return domain.Transfer{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO idempotency_records(idempotency_key,request_fingerprint,transfer_id,created_at) VALUES($1,$2,$3,$4)`, req.IdempotencyKey, fingerprint, id, now); err != nil {
		return domain.Transfer{}, err
	}

	// Stage 5: lock the affected wallets in a deterministic order and read balances.
	balances, err := lockWallets(ctx, tx, req.FromWalletID, req.ToWalletID)
	if err != nil {
		return domain.Transfer{}, err
	}
	if balances[req.FromWalletID] < req.Amount {
		// Stage 6a: persist a replay-safe failed outcome without ledger side effects.
		transfer.Status, transfer.FailureReason = domain.Failed, domain.ErrInsufficientFunds.Error()
		if _, err := tx.ExecContext(ctx, "UPDATE transfers SET status=$1, failure_reason=$2 WHERE id=$3", transfer.Status, transfer.FailureReason, id); err != nil {
			return domain.Transfer{}, err
		}
		if err := tx.Commit(); err != nil {
			return domain.Transfer{}, err
		}
		return transfer, domain.ErrInsufficientFunds
	}

	// Stage 6b: apply the balance updates while both wallet rows remain locked.
	if _, err := tx.ExecContext(ctx, "UPDATE wallets SET balance=balance-$1 WHERE id=$2", req.Amount, req.FromWalletID); err != nil {
		return domain.Transfer{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE wallets SET balance=balance+$1 WHERE id=$2", req.Amount, req.ToWalletID); err != nil {
		return domain.Transfer{}, err
	}

	// Stage 7: write the immutable debit and credit ledger entries as a pair.
	debitEntryID, creditEntryID := uuid.NewString(), uuid.NewString()
	if _, err := tx.ExecContext(ctx, `INSERT INTO ledger_entries(id,transfer_id,wallet_id,entry_type,amount,created_at) VALUES($1,$2,$3,'DEBIT',$4,$5),($6,$2,$7,'CREDIT',$4,$5)`, debitEntryID, id, req.FromWalletID, req.Amount, now, creditEntryID, req.ToWalletID); err != nil {
		return domain.Transfer{}, err
	}

	// Stage 8: move the transfer to its terminal processed state and commit atomically.
	transfer.Status = domain.Processed
	if _, err := tx.ExecContext(ctx, "UPDATE transfers SET status=$1 WHERE id=$2 AND status=$3", transfer.Status, id, domain.Pending); err != nil {
		return domain.Transfer{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Transfer{}, err
	}
	return transfer, nil
}

// replayAfterDuplicate handles the race where another request committed the same
// idempotency key after this request performed its initial lookup. It reads that
// committed result rather than creating a second transfer.
func (r *PostgresRepository) replayAfterDuplicate(ctx context.Context, key, fingerprint string) (domain.Transfer, error) {
	// Stage 1: read the winner's committed idempotency record outside the rolled-back transaction.
	existing, found, err := getByKey(ctx, r.db, key)
	if err != nil {
		return domain.Transfer{}, err
	}
	if !found {
		return domain.Transfer{}, fmt.Errorf("idempotency record disappeared after unique constraint violation")
	}
	// Stage 2: reject a key that was reused for different transfer input.
	if existing.fingerprint != fingerprint {
		return domain.Transfer{}, domain.ErrIdempotencyConflict
	}
	// Stage 3: return the winner's stored outcome without repeating any side effect.
	existing.transfer.Replayed = true
	if existing.transfer.Status == domain.Failed {
		return existing.transfer, domain.ErrInsufficientFunds
	}
	return existing.transfer, nil
}

// lockWallets locks both wallets in a stable order to avoid deadlocks.
func lockWallets(ctx context.Context, tx *sql.Tx, first, second string) (map[string]int64, error) {
	ids := []string{first, second}
	sort.Strings(ids)
	balances := make(map[string]int64, 2)
	for _, id := range ids {
		var balance int64
		err := tx.QueryRowContext(ctx, "SELECT balance FROM wallets WHERE id=$1 FOR UPDATE", id).Scan(&balance)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		if err != nil {
			return nil, err
		}
		balances[id] = balance
	}
	return balances, nil
}

type rowQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type storedTransfer struct {
	transfer    domain.Transfer
	fingerprint string
}

func getByKey(ctx context.Context, q rowQueryer, key string) (storedTransfer, bool, error) {
	var s storedTransfer
	err := q.QueryRowContext(ctx, `SELECT t.id,t.idempotency_key,t.from_wallet_id,t.to_wallet_id,t.amount,t.status,COALESCE(t.failure_reason,''),i.request_fingerprint FROM transfers t JOIN idempotency_records i ON i.transfer_id=t.id WHERE i.idempotency_key=$1`, key).Scan(&s.transfer.ID, &s.transfer.IdempotencyKey, &s.transfer.FromWalletID, &s.transfer.ToWalletID, &s.transfer.Amount, &s.transfer.Status, &s.transfer.FailureReason, &s.fingerprint)
	if errors.Is(err, sql.ErrNoRows) {
		return storedTransfer{}, false, nil
	}
	return s, err == nil, err
}
func requestFingerprint(r domain.CreateTransferRequest) string {
	sum := sha256.Sum256([]byte(r.FromWalletID + "\\x00" + r.ToWalletID + "\\x00" + fmt.Sprint(r.Amount)))
	return hex.EncodeToString(sum[:])
}
func newTransferID() (string, error) {
	return uuid.NewString(), nil
}
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// isRetryableTransaction identifies PostgreSQL errors that are safe to retry as a whole.
func isRetryableTransaction(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "40001" || pgErr.Code == "40P01")
}

// LedgerEntryCount supports behavioral assertions about a transfer's ledger pair.
func (r *PostgresRepository) LedgerEntryCount(ctx context.Context, transferID string) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, "SELECT count(*) FROM ledger_entries WHERE transfer_id=$1", transferID).Scan(&n)
	return n, err
}

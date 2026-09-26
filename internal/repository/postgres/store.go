package postgres

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"wallet-transfer-assignment/internal/domain"

	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

const advisoryMigrationLock int64 = 748291043817

type Store struct {
	db                 *sql.DB
	maxClaimAttempts   int
	claimRetryInterval time.Duration
}

func Open(ctx context.Context, databaseURL string, maxClaimAttempts int) (*Store, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	if maxClaimAttempts < 1 {
		return nil, errors.New("MAX_CLAIM_ATTEMPTS must be at least 1")
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to postgres database: %w", err)
	}

	store := &Store{db: db, maxClaimAttempts: maxClaimAttempts, claimRetryInterval: 40 * time.Millisecond}
	if err := store.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, advisoryMigrationLock); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version BIGINT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`); err != nil {
		return fmt.Errorf("create migration history: %w", err)
	}

	files, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(files)
	for _, file := range files {
		base := filepath.Base(file)
		versionText := strings.SplitN(base, "_", 2)[0]
		version, err := strconv.ParseInt(versionText, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid migration filename %q: %w", base, err)
		}
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&exists); err != nil {
			return fmt.Errorf("check migration %d: %w", version, err)
		}
		if exists {
			continue
		}
		contents, err := migrationFiles.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", file, err)
		}
		for _, statement := range strings.Split(string(contents), ";") {
			statement = strings.TrimSpace(statement)
			if statement == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("apply migration %s: %w", file, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES ($1) ON CONFLICT (version) DO NOTHING`, version); err != nil {
			return fmt.Errorf("record migration %d: %w", version, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	return nil
}

func (s *Store) CreateWallet(ctx context.Context, walletID string, openingBalance int64) (domain.Wallet, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Wallet{}, fmt.Errorf("begin wallet creation: %w", err)
	}
	defer tx.Rollback()

	var wallet domain.Wallet
	err = tx.QueryRowContext(ctx, `
INSERT INTO wallets(wallet_id, balance) VALUES ($1, $2)
RETURNING wallet_id, balance`, walletID, openingBalance).Scan(&wallet.ID, &wallet.Balance)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.Wallet{}, domain.ErrWalletExists
		}
		return domain.Wallet{}, fmt.Errorf("create wallet: %w", err)
	}
	if openingBalance > 0 {
		operationID := "opening:" + walletID
		for _, entry := range []struct {
			accountID string
			walletID  any
			entryType string
		}{{"system:funding", nil, "DEBIT"}, {walletID, walletID, "CREDIT"}} {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO ledger_entries(account_id, wallet_id, transfer_id, operation_id, entry_type, amount)
VALUES ($1, $2, NULL, $3, $4, $5)`, entry.accountID, entry.walletID, operationID, entry.entryType, openingBalance); err != nil {
				return domain.Wallet{}, fmt.Errorf("write wallet opening ledger entry: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.Wallet{}, fmt.Errorf("commit wallet creation: %w", err)
	}
	return wallet, nil
}

func (s *Store) GetWallet(ctx context.Context, walletID string) (domain.Wallet, error) {
	var wallet domain.Wallet
	err := s.db.QueryRowContext(ctx, `SELECT wallet_id, balance FROM wallets WHERE wallet_id = $1`, walletID).
		Scan(&wallet.ID, &wallet.Balance)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Wallet{}, domain.NewWalletNotFoundError(walletID)
	}
	if err != nil {
		return domain.Wallet{}, fmt.Errorf("get wallet: %w", err)
	}
	return wallet, nil
}

func (s *Store) ListTransfers(ctx context.Context, walletID string, limit int) ([]domain.Transfer, error) {
	var exists bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM wallets WHERE wallet_id = $1)`, walletID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check wallet for history: %w", err)
	}
	if !exists {
		return nil, domain.NewWalletNotFoundError(walletID)
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT transfer_id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, created_at, failure_reason
FROM transfers
WHERE from_wallet_id = $1 OR to_wallet_id = $1
ORDER BY created_at DESC, transfer_id DESC
LIMIT $2`, walletID, limit)
	if err != nil {
		return nil, fmt.Errorf("list wallet transfer history: %w", err)
	}
	defer rows.Close()
	transfers := make([]domain.Transfer, 0)
	for rows.Next() {
		transfer, err := scanTransfer(rows)
		if err != nil {
			return nil, fmt.Errorf("scan transfer history: %w", err)
		}
		transfers = append(transfers, transfer)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read transfer history: %w", err)
	}
	return transfers, nil
}

func scanTransfer(row interface{ Scan(...any) error }) (domain.Transfer, error) {
	var transfer domain.Transfer
	var status string
	err := row.Scan(&transfer.ID, &transfer.IdempotencyKey, &transfer.FromWalletID, &transfer.ToWalletID,
		&transfer.Amount, &status, &transfer.CreatedAt, &transfer.FailureReason)
	transfer.Status = domain.TransferStatus(status)
	return transfer, err
}

func isUniqueViolation(err error) bool {
	var pgError interface{ SQLState() string }
	return errors.As(err, &pgError) && pgError.SQLState() == "23505"
}

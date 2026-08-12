package postgres

import (
	"context"
	"fmt"
	"math/rand"
	"net/url"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/devthedeveloper/wallet-transfer-assignment/internal/service"
)

// Config controls connection pooling and timeout behaviour.
type Config struct {
	DatabaseURL      string
	MaxConns         int32
	StatementTimeout time.Duration
	LockTimeout      time.Duration
}

// DB wraps a *gorm.DB and implements service.UnitOfWork. GORM is used here
// as a typed connection/transaction manager and for simple reads — it is
// deliberately NOT trusted to generate the SQL for anything
// concurrency-critical (locking a wallet, the guarded balance update, the
// idempotency claim). Those go through .Raw()/.Exec() with the exact SQL
// text this service has always used, so the locking guarantees in
// docs/design.md §7 don't depend on an ORM's query-builder defaults. See
// docs/design.md §13 for the full rationale.
type DB struct {
	gorm        *gorm.DB
	lockTimeout time.Duration
}

// NewDB opens a GORM connection over Postgres. statement_timeout is applied
// per-connection via the DSN's `options` parameter (GORM/pgx's stdlib
// driver pools through database/sql, which has no per-connect hook the way
// pgxpool did, so this is the equivalent mechanism).
func NewDB(ctx context.Context, cfg Config) (*DB, error) {
	dsn, err := withStatementTimeout(cfg.DatabaseURL, cfg.StatementTimeout)
	if err != nil {
		return nil, fmt.Errorf("build dsn: %w", err)
	}

	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.Silent),
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return nil, fmt.Errorf("open gorm connection: %w", err)
	}

	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, fmt.Errorf("get underlying sql.DB: %w", err)
	}
	if cfg.MaxConns > 0 {
		sqlDB.SetMaxOpenConns(int(cfg.MaxConns))
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return &DB{gorm: gdb, lockTimeout: cfg.LockTimeout}, nil
}

func withStatementTimeout(rawURL string, statementTimeout time.Duration) (string, error) {
	if statementTimeout <= 0 {
		return rawURL, nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse database url: %w", err)
	}
	q := u.Query()
	stmtOpt := fmt.Sprintf("-c statement_timeout=%d", statementTimeout.Milliseconds())
	if existing := q.Get("options"); existing != "" {
		stmtOpt = existing + " " + stmtOpt
	}
	q.Set("options", stmtOpt)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Gorm exposes the underlying *gorm.DB for the migrator and for direct SQL
// in tests. Not used by application code outside this package.
func (db *DB) Gorm() *gorm.DB { return db.gorm }

// Close releases the underlying connection pool.
func (db *DB) Close() error {
	sqlDB, err := db.gorm.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Ping verifies the database is reachable.
func (db *DB) Ping(ctx context.Context) error {
	sqlDB, err := db.gorm.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

// Wallets, Transfers and Ledger implement service.Repos for
// non-transactional reads (e.g. the idempotency replay fast path), backed
// directly by the connection pool.
func (db *DB) Wallets() service.WalletRepository     { return &walletRepository{q: db.gorm} }
func (db *DB) Transfers() service.TransferRepository { return &transferRepository{q: db.gorm} }
func (db *DB) Ledger() service.LedgerRepository      { return &ledgerRepository{q: db.gorm} }

const (
	maxTxAttempts  = 3
	retryBaseDelay = 10 * time.Millisecond
)

// Do implements service.UnitOfWork. It runs fn inside a transaction,
// retrying the whole transaction with jittered backoff on serialization
// failures (40001) and deadlocks (40P01) — see docs/design.md §7. Retrying
// is safe here because fn's only side effects are the repository calls it
// makes through the Repos it is given, and the whole operation is
// idempotent by construction (guarded by the idempotency key and the
// guarded balance updates).
func (db *DB) Do(ctx context.Context, fn func(ctx context.Context, r service.Repos) error) error {
	var lastErr error
	for attempt := 1; attempt <= maxTxAttempts; attempt++ {
		err := db.doOnce(ctx, fn)
		if err == nil {
			return nil
		}
		lastErr = err
		if !isRetryableCode(err) || attempt == maxTxAttempts {
			return err
		}
		//nolint:gosec // jitter for retry backoff timing, not a security-sensitive value
		backoff := time.Duration(attempt)*retryBaseDelay + time.Duration(rand.Intn(int(retryBaseDelay.Milliseconds())))*time.Millisecond
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return lastErr
}

func (db *DB) doOnce(ctx context.Context, fn func(ctx context.Context, r service.Repos) error) error {
	return db.gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if db.lockTimeout > 0 {
			// SET LOCAL doesn't support bind parameters, so this is a
			// formatted literal rather than a `?`-parameterized Exec.
			stmt := fmt.Sprintf("SET LOCAL lock_timeout = '%dms'", db.lockTimeout.Milliseconds())
			if err := tx.Exec(stmt).Error; err != nil {
				return fmt.Errorf("set lock_timeout: %w", err)
			}
		}

		repos := &txRepos{
			wallets:   &walletRepository{q: tx},
			transfers: &transferRepository{q: tx},
			ledger:    &ledgerRepository{q: tx},
		}
		return fn(ctx, repos)
	})
}

type txRepos struct {
	wallets   service.WalletRepository
	transfers service.TransferRepository
	ledger    service.LedgerRepository
}

func (r *txRepos) Wallets() service.WalletRepository     { return r.wallets }
func (r *txRepos) Transfers() service.TransferRepository { return r.transfers }
func (r *txRepos) Ledger() service.LedgerRepository      { return r.ledger }

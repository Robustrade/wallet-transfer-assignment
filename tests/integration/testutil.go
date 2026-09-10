package integration

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/handler"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestApp encapsulates all dependencies needed for integration tests.
type TestApp struct {
	Pool            *pgxpool.Pool
	TransferService *service.TransferService
	WalletService   *service.WalletService
	Router          *chi.Mux
	Logger          *slog.Logger
}

// SetupTestApp creates a test application with a real PostgreSQL database.
func SetupTestApp(t *testing.T) *TestApp {
	t.Helper()
	ctx := context.Background()

	// Start PostgreSQL container
	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("wallet_transfer_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}

	t.Cleanup(func() {
		if cleanupErr := pgContainer.Terminate(ctx); cleanupErr != nil {
			t.Logf("failed to terminate postgres container: %v", cleanupErr)
		}
	})

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	// Create connection pool
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	// Run migrations
	runMigrations(t, pool)

	// Create logger
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Initialize repositories
	txManager := repository.NewTxManager(pool)
	walletRepo := repository.NewWalletRepository()
	transferRepo := repository.NewTransferRepository()
	ledgerRepo := repository.NewLedgerRepository()

	// Initialize services
	transferSvc := service.NewTransferService(txManager, walletRepo, transferRepo, ledgerRepo, logger)
	walletSvc := service.NewWalletService(txManager, walletRepo, ledgerRepo, logger)

	// Initialize handlers and router
	transferHandler := handler.NewTransferHandler(transferSvc, logger)
	walletHandler := handler.NewWalletHandler(walletSvc, logger)

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		transferHandler.RegisterRoutes(r)
		walletHandler.RegisterRoutes(r)
	})

	return &TestApp{
		Pool:            pool,
		TransferService: transferSvc,
		WalletService:   walletSvc,
		Router:          r,
		Logger:          logger,
	}
}

// runMigrations executes all SQL migration files in order.
func runMigrations(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	migrationsDir := getMigrationsDir()

	// Execute migration files in order
	migrationFiles := []string{
		"000001_create_wallets.up.sql",
		"000002_create_transfers.up.sql",
		"000003_create_ledger_entries.up.sql",
	}

	for _, file := range migrationFiles {
		path := filepath.Join(migrationsDir, file)
		sql, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read migration file %s: %v", file, err)
		}

		_, err = pool.Exec(ctx, string(sql))
		if err != nil {
			t.Fatalf("failed to execute migration %s: %v", file, err)
		}
	}
}

// getMigrationsDir returns the path to the migrations directory.
func getMigrationsDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "..", "..", "migrations")
}

// SeedWallets creates test wallets for integration tests.
func SeedWallets(t *testing.T, app *TestApp) {
	t.Helper()
	ctx := context.Background()

	wallets := []struct {
		id      string
		balance int64
	}{
		{"wallet_1", 100000}, // $1000.00
		{"wallet_2", 50000},  // $500.00
		{"wallet_3", 25000},  // $250.00
	}

	for _, w := range wallets {
		_, err := app.Pool.Exec(ctx,
			`INSERT INTO wallets (id, balance, currency) VALUES ($1, $2, 'USD') ON CONFLICT (id) DO NOTHING`,
			w.id, w.balance,
		)
		if err != nil {
			t.Fatalf("failed to seed wallet %s: %v", w.id, err)
		}
	}
}

// CleanupData removes all test data between tests.
func CleanupData(t *testing.T, app *TestApp) {
	t.Helper()
	ctx := context.Background()

	tables := []string{"ledger_entries", "transfers", "wallets"}
	for _, table := range tables {
		_, err := app.Pool.Exec(ctx, fmt.Sprintf("DELETE FROM %s", table))
		if err != nil {
			t.Fatalf("failed to cleanup table %s: %v", table, err)
		}
	}
}

// GetWalletBalance retrieves the current balance for a wallet directly from the DB.
func GetWalletBalance(t *testing.T, app *TestApp, walletID string) int64 {
	t.Helper()
	var balance int64
	err := app.Pool.QueryRow(context.Background(),
		`SELECT balance FROM wallets WHERE id = $1`, walletID,
	).Scan(&balance)
	if err != nil {
		t.Fatalf("failed to get wallet balance for %s: %v", walletID, err)
	}
	return balance
}

// CountLedgerEntries returns the number of ledger entries for a transfer.
func CountLedgerEntries(t *testing.T, app *TestApp, transferID string) int {
	t.Helper()
	var count int
	err := app.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = $1`, transferID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count ledger entries: %v", err)
	}
	return count
}

//go:build integration

package integration

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/prateekgautam/wallet-transfer-assignment/internal/handler"
	"github.com/prateekgautam/wallet-transfer-assignment/internal/repository"
	"github.com/prateekgautam/wallet-transfer-assignment/internal/service"
)

var (
	testServer *httptest.Server
	testDB     *repository.DB
)

// TestMain sets up the integration test environment: connects to a real PostgreSQL
// database, runs migrations, wires all dependencies, and starts an httptest server.
func TestMain(m *testing.M) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://wallet:wallet_secret@localhost:5432/wallet_transfer_test?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Connect to test database
	var err error
	testDB, err = repository.NewDB(ctx, dbURL)
	if err != nil {
		logger.Error("failed to connect to test database — make sure PostgreSQL is running", "error", err)
		os.Exit(1)
	}
	defer testDB.Close()

	// Run migrations
	if err := testDB.RunMigrations(ctx, "../../migrations"); err != nil {
		logger.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	// Wire dependencies
	walletRepo := repository.NewWalletRepository()
	transferRepo := repository.NewTransferRepository()
	ledgerRepo := repository.NewLedgerRepository()
	idempotencyRepo := repository.NewIdempotencyRepository()

	svc := service.NewTransferService(testDB, walletRepo, transferRepo, ledgerRepo, idempotencyRepo, logger)

	transferHandler := handler.NewTransferHandler(svc, logger)
	walletHandler := handler.NewWalletHandler(svc, logger)

	// Set up router (same routes as production)
	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.Recoverer)

	r.Route("/transfers", func(r chi.Router) {
		r.Post("/", transferHandler.CreateTransfer)
		r.Get("/{id}", transferHandler.GetTransfer)
	})
	r.Route("/wallets", func(r chi.Router) {
		r.Post("/", walletHandler.CreateWallet)
		r.Get("/{id}", walletHandler.GetWallet)
	})

	testServer = httptest.NewServer(r)
	defer testServer.Close()

	code := m.Run()
	os.Exit(code)
}

// cleanupDB removes all test data between tests in reverse FK order.
// No circular FK exists, so straightforward deletion works.
func cleanupDB(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	queries := []string{
		"DELETE FROM ledger_entries",
		"DELETE FROM transfers",
		"DELETE FROM idempotency_records",
		"DELETE FROM wallets",
	}
	for _, q := range queries {
		if _, err := testDB.Pool.Exec(ctx, q); err != nil {
			t.Fatalf("cleanup failed on %q: %v", q, err)
		}
	}
}

// createTestWallet inserts a wallet directly into the database for test setup.
func createTestWallet(t *testing.T, id string, balance int64) {
	t.Helper()
	ctx := context.Background()
	_, err := testDB.Pool.Exec(ctx,
		"INSERT INTO wallets (id, balance, currency) VALUES ($1, $2, 'USD')",
		id, balance,
	)
	if err != nil {
		t.Fatalf("failed to create test wallet %s: %v", id, err)
	}
}

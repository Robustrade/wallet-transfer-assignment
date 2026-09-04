package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"wallet-transfer-assignment/internal/config"
	"wallet-transfer-assignment/internal/database"
	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/handler"
	"wallet-transfer-assignment/internal/repository/postgres"
	"wallet-transfer-assignment/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type TestSuite struct {
	DB              *database.DB
	WalletRepo      *postgres.WalletRepository
	TransferRepo    *postgres.TransferRepository
	LedgerRepo      *postgres.LedgerRepository
	IdempotencyRepo *postgres.IdempotencyRepository
	WalletService   *service.WalletService
	TransferService *service.TransferService
	Router          *gin.Engine
}

func SetupTestSuite(t *testing.T) *TestSuite {
	t.Helper()

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/wallet_test_db?sslmode=disable"
	}

	cfg := &config.Config{
		DatabaseURL: dbURL,
		AutoMigrate: false,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := database.NewPostgresPool(ctx, cfg)
	if err != nil {
		t.Skipf("Skipping integration test; could not connect to test PostgreSQL database at %s: %v", dbURL, err)
		return nil
	}

	// Locate migrations directory relative to test directory
	migrationsPath := filepath.Join("..", "..", "migrations")
	if _, err := os.Stat(migrationsPath); os.IsNotExist(err) {
		migrationsPath = "migrations"
	}

	// Run migrations down and up for clean test environment
	_ = db.RunMigrations(dbURL, migrationsPath)

	transactor := postgres.NewTransactor(db.Pool)
	walletRepo := postgres.NewWalletRepository(db.Pool)
	transferRepo := postgres.NewTransferRepository(db.Pool)
	ledgerRepo := postgres.NewLedgerRepository(db.Pool)
	idempotencyRepo := postgres.NewIdempotencyRepository(db.Pool)

	walletService := service.NewWalletService(walletRepo, ledgerRepo)
	transferService := service.NewTransferService(transactor, walletRepo, transferRepo, ledgerRepo, idempotencyRepo)

	transferHandler := handler.NewTransferHandler(transferService)
	walletHandler := handler.NewWalletHandler(walletService)
	healthHandler := handler.NewHealthHandler(db)

	router := handler.NewRouter(transferHandler, walletHandler, healthHandler)

	// Clean database tables before test execution
	CleanDatabase(t, db)

	return &TestSuite{
		DB:              db,
		WalletRepo:      walletRepo,
		TransferRepo:    transferRepo,
		LedgerRepo:      ledgerRepo,
		IdempotencyRepo: idempotencyRepo,
		WalletService:   walletService,
		TransferService: transferService,
		Router:          router,
	}
}

func CleanDatabase(t *testing.T, db *database.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	queries := []string{
		"TRUNCATE TABLE idempotency_records CASCADE;",
		"TRUNCATE TABLE ledger_entries CASCADE;",
		"TRUNCATE TABLE transfers CASCADE;",
		"TRUNCATE TABLE wallets CASCADE;",
	}

	for _, q := range queries {
		_, err := db.Pool.Exec(ctx, q)
		require.NoError(t, err, "failed truncating tables for test setup")
	}
}

func CreateTestWallet(t *testing.T, ts *TestSuite, initialBalance string) *domain.Wallet {
	t.Helper()
	ctx := context.Background()
	bal, err := domain.ParseMoney(initialBalance)
	require.NoError(t, err)
	wallet, err := ts.WalletService.CreateWallet(ctx, bal, "USD")
	require.NoError(t, err)
	return wallet
}

package testutil

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Robustrade/wallet-transfer-assignment/internal/database"
)

func RunWithDatabase(m *testing.M) int {
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("wallet_test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		if os.Getenv("SKIP_DB_TESTS") == "1" {
			log.Printf("SKIPPING database tests (SKIP_DB_TESTS=1): could not start a Postgres container: %v", err)
			return 0
		}
		log.Printf("could not start a Postgres container for tests (is Docker running? set SKIP_DB_TESTS=1 to skip): %v", err)
		return 1
	}
	defer func() { _ = container.Terminate(ctx) }()

	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Printf("get test database URL: %v", err)
		return 1
	}
	if err := database.Migrate(url); err != nil {
		log.Printf("migrate test database: %v", err)
		return 1
	}
	if err := database.Connect(url); err != nil {
		log.Printf("connect test database: %v", err)
		return 1
	}

	return m.Run()
}

func ResetDatabase(t *testing.T) {
	t.Helper()
	err := database.DB.Exec("TRUNCATE ledger_entries, transfers, idempotency_records, wallets RESTART IDENTITY CASCADE").Error
	if err != nil {
		t.Fatalf("reset test database: %v", err)
	}
}

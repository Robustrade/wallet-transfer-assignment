package testutil

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-transfer-assignment/internal/repository/postgres"
)

func getAdminURL(dbURL string) string {
	if adminURL := os.Getenv("TEST_ADMIN_URL"); adminURL != "" {
		return adminURL
	}
	u, err := url.Parse(dbURL)
	if err != nil {
		return "postgres://postgres:postgrespassword@localhost:5432/wallet_test_db?sslmode=disable"
	}
	u.User = url.UserPassword("postgres", "postgrespassword")
	return u.String()
}

// ensureTestDatabaseExists creates the test database if it does not exist, protecting application databases.
func ensureTestDatabaseExists(dbURL string) error {
	adminURL := getAdminURL(dbURL)
	u, err := url.Parse(adminURL)
	if err != nil {
		return fmt.Errorf("failed to parse admin db URL: %w", err)
	}
	dbName := strings.TrimPrefix(u.Path, "/")
	if dbName == "" || dbName == "postgres" || dbName == "wallet_db" {
		return nil
	}

	// Connect to default "postgres" administrative database
	adminRootURL := *u
	adminRootURL.Path = "/postgres"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	adminPool, err := pgxpool.New(ctx, adminRootURL.String())
	if err != nil {
		return fmt.Errorf("failed to connect to admin database for test db initialization: %w", err)
	}
	defer adminPool.Close()

	var exists bool
	if err := adminPool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", dbName).Scan(&exists); err != nil {
		return fmt.Errorf("failed to check database existence: %w", err)
	}
	if !exists {
		if _, err := adminPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", pgx.Identifier{dbName}.Sanitize())); err != nil {
			return fmt.Errorf("failed to create test database %s: %w", dbName, err)
		}
	}
	return nil
}

// SetupTestDB initializes connection to the PostgreSQL test instance, runs migrations, and registers cleanup.
func SetupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://wallet_app:wallet_app_password@localhost:5432/wallet_test_db?sslmode=disable"
	}

	u, parseErr := url.Parse(dbURL)
	if parseErr != nil {
		t.Fatalf("invalid TEST_DATABASE_URL parse error: %v", parseErr)
	}

	sanitizedEndpoint := "localhost:5432/wallet_test_db"
	if u.Host != "" {
		sanitizedEndpoint = u.Host + u.Path
	}

	dbName := strings.TrimPrefix(u.Path, "/")
	lowerName := strings.ToLower(dbName)
	// Explicit allowlist / verification: must be "wallet_test_db" or end with "_test"/"_test_db", and must NOT contain "prod"
	isAllowedTestDB := (lowerName == "wallet_test_db" || strings.HasSuffix(lowerName, "_test") || strings.HasSuffix(lowerName, "_test_db")) &&
		!strings.Contains(lowerName, "prod") && !strings.Contains(lowerName, "production")

	if !isAllowedTestDB || dbName == "wallet_db" || dbName == "postgres" {
		t.Fatalf("refusing to run destructive test setup on unauthorized database: %s. TEST_DATABASE_URL must target an authorized test database (e.g. wallet_test_db)", sanitizedEndpoint)
	}
	if appURL := os.Getenv("DATABASE_URL"); appURL != "" {
		if appU, appErr := url.Parse(appURL); appErr == nil {
			if strings.EqualFold(appU.Host, u.Host) && strings.EqualFold(strings.TrimPrefix(appU.Path, "/"), dbName) {
				t.Fatalf("TEST_DATABASE_URL matches application DATABASE_URL (%s); refusing to truncate application database", sanitizedEndpoint)
			}
		}
	}

	if err := ensureTestDatabaseExists(dbURL); err != nil {
		t.Fatalf("failed to ensure test database exists: %v", err)
	}

	ctx := context.Background()

	// Connect admin pool for serialized advisory locking, migrations, and cleanup
	adminURL := getAdminURL(dbURL)
	adminPool, err := postgres.NewPool(ctx, adminURL)
	if err != nil {
		t.Fatalf("failed to connect to admin database at %s: %v", sanitizedEndpoint, err)
	}

	lockConn, err := adminPool.Acquire(ctx)
	if err != nil {
		adminPool.Close()
		t.Fatalf("failed to acquire connection for test advisory lock: %v", err)
	}
	if _, err := lockConn.Exec(ctx, "SELECT pg_advisory_lock(777888);"); err != nil {
		_, _ = lockConn.Exec(context.Background(), "SELECT pg_advisory_unlock(777888);")
		lockConn.Release()
		adminPool.Close()
		t.Fatalf("failed to acquire pg_advisory_lock: %v", err)
	}

	if err := postgres.Migrate(ctx, adminPool); err != nil {
		_, _ = lockConn.Exec(context.Background(), "SELECT pg_advisory_unlock(777888);")
		lockConn.Release()
		adminPool.Close()
		t.Fatalf("failed to migrate test database: %v", err)
	}

	// Truncate all tables as admin to guarantee test isolation across tests
	cleanupQuery := `
		SET session_replication_role = 'replica';
		TRUNCATE TABLE ledger_entries, transfers, idempotency_records, wallets CASCADE;
		SET session_replication_role = 'origin';
		INSERT INTO wallets (id, name, balance, currency)
		VALUES ('system_treasury', 'System Treasury', 100000000000000, 'USD')
		ON CONFLICT (id) DO NOTHING;
		INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount)
		VALUES ('entry_system_treasury_opening', NULL, 'system_treasury', 'CREDIT', 100000000000000)
		ON CONFLICT (id) DO NOTHING;
	`
	if _, err := lockConn.Exec(ctx, cleanupQuery); err != nil {
		_, _ = lockConn.Exec(context.Background(), "SELECT pg_advisory_unlock(777888);")
		lockConn.Release()
		adminPool.Close()
		t.Fatalf("failed to clean test tables: %v", err)
	}

	// Connect runtime pool for the application under test as wallet_app
	pool, err := postgres.NewPool(ctx, dbURL)
	if err != nil {
		_, _ = lockConn.Exec(context.Background(), "SELECT pg_advisory_unlock(777888);")
		lockConn.Release()
		adminPool.Close()
		t.Fatalf("failed to connect to PostgreSQL at %s: %v. Integration tests require a running PostgreSQL instance.", sanitizedEndpoint, err)
		return nil
	}

	t.Cleanup(func() {
		pool.Close()
		_, _ = lockConn.Exec(context.Background(), "SELECT pg_advisory_unlock(777888);")
		lockConn.Release()
		adminPool.Close()
	})

	return pool
}

// SetupAdminDB initializes an administrative connection (schema owner) to the test database,
// used for migration testing and privileged schema validation.
func SetupAdminDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://wallet_app:wallet_app_password@localhost:5432/wallet_test_db?sslmode=disable"
	}
	adminURL := getAdminURL(dbURL)

	if err := ensureTestDatabaseExists(dbURL); err != nil {
		t.Fatalf("failed to ensure test database exists: %v", err)
	}

	ctx := context.Background()
	adminPool, err := postgres.NewPool(ctx, adminURL)
	if err != nil {
		t.Fatalf("failed to connect to admin database: %v", err)
	}

	lockConn, err := adminPool.Acquire(ctx)
	if err != nil {
		adminPool.Close()
		t.Fatalf("failed to acquire connection for test advisory lock: %v", err)
	}
	if _, err := lockConn.Exec(ctx, "SELECT pg_advisory_lock(777888);"); err != nil {
		_, _ = lockConn.Exec(context.Background(), "SELECT pg_advisory_unlock(777888);")
		lockConn.Release()
		adminPool.Close()
		t.Fatalf("failed to acquire pg_advisory_lock: %v", err)
	}

	cleanupQuery := `
		SET session_replication_role = 'replica';
		TRUNCATE TABLE ledger_entries, transfers, idempotency_records, wallets CASCADE;
		SET session_replication_role = 'origin';
		INSERT INTO wallets (id, name, balance, currency)
		VALUES ('system_treasury', 'System Treasury', 100000000000000, 'USD')
		ON CONFLICT (id) DO NOTHING;
		INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount)
		VALUES ('entry_system_treasury_opening', NULL, 'system_treasury', 'CREDIT', 100000000000000)
		ON CONFLICT (id) DO NOTHING;
	`
	if _, err := lockConn.Exec(ctx, cleanupQuery); err != nil {
		_, _ = lockConn.Exec(context.Background(), "SELECT pg_advisory_unlock(777888);")
		lockConn.Release()
		adminPool.Close()
		t.Fatalf("failed to clean test tables: %v", err)
	}

	t.Cleanup(func() {
		_, _ = lockConn.Exec(context.Background(), "SELECT pg_advisory_unlock(777888);")
		lockConn.Release()
		adminPool.Close()
	})

	return adminPool
}

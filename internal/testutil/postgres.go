package testutil

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/amantiwari/wallet-transfer/internal/adapter/postgres"
)

var (
	startOnce sync.Once
	startErr  error

	admin     *pgxpool.Pool
	port      int
	dataDir   string
	socketDir string
	binDir    string
	managed   bool
	sharedDSN string
)

// DSN returns a migrated PostgreSQL database for a test.
// Set WALLET_TEST_DATABASE_URL to use an existing server. Otherwise a temporary
// cluster is started from local PostgreSQL binaries.
func DSN(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping database test in short mode")
	}
	startOnce.Do(func() {
		startErr = startCluster()
	})
	if startErr != nil {
		t.Fatalf("start postgres: %v", startErr)
	}
	if !managed {
		truncateShared(t)
		return sharedDSN
	}
	return cloneTemplate(t)
}

// Shutdown stops a temporary cluster. It is safe to call when no cluster started.
func Shutdown() {
	if admin != nil {
		admin.Close()
		admin = nil
	}
	if !managed || dataDir == "" || binDir == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(binDir, "pg_ctl"), "-D", dataDir, "-m", "immediate", "stop")
	_ = cmd.Run()
	_ = os.RemoveAll(socketDir)
	_ = os.RemoveAll(dataDir)
	managed = false
}

func startCluster() error {
	if dsn := strings.TrimSpace(os.Getenv("WALLET_TEST_DATABASE_URL")); dsn != "" {
		return useExternal(dsn)
	}
	dir, err := postgresBinDir()
	if err != nil {
		return err
	}
	binDir = dir
	root, err := os.MkdirTemp("", "walletpg")
	if err != nil {
		return err
	}
	dataDir = filepath.Join(root, "data")
	socketDir, err = os.MkdirTemp("/tmp", "wpg")
	if err != nil {
		socketDir = filepath.Join(root, "sock")
		if mkErr := os.MkdirAll(socketDir, 0o700); mkErr != nil {
			return mkErr
		}
	}
	if err := run(filepath.Join(binDir, "initdb"), "-D", dataDir, "-U", "wallet", "--auth=trust", "--encoding=UTF8", "--locale=C", "--no-sync"); err != nil {
		return err
	}
	port, err = freePort()
	if err != nil {
		return err
	}
	logPath := filepath.Join(root, "postgres.log")
	options := fmt.Sprintf("-p %d -k %s -c listen_addresses=127.0.0.1 -c unix_socket_directories=%s -c max_connections=200 -c fsync=off -c synchronous_commit=off -c full_page_writes=off", port, socketDir, socketDir)
	if err := run(filepath.Join(binDir, "pg_ctl"), "-D", dataDir, "-l", logPath, "-w", "-o", options, "start"); err != nil {
		logBody, _ := os.ReadFile(logPath)
		return fmt.Errorf("%w\n%s", err, logBody)
	}
	managed = true

	adminPool, err := connectFirst(port)
	if err != nil {
		logBody, _ := os.ReadFile(logPath)
		return fmt.Errorf("%w\n%s", err, logBody)
	}
	admin = adminPool
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := admin.Exec(ctx, "CREATE DATABASE wallet_template"); err != nil {
		return fmt.Errorf("create template database: %w", err)
	}
	templateDSN := fmt.Sprintf("postgres://wallet@127.0.0.1:%d/wallet_template?sslmode=disable", port)
	if err := migrate(ctx, templateDSN); err != nil {
		return err
	}
	return nil
}

func useExternal(dsn string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect WALLET_TEST_DATABASE_URL: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return fmt.Errorf("ping WALLET_TEST_DATABASE_URL: %w", err)
	}
	admin = pool
	sharedDSN = dsn
	managed = false
	return migrate(ctx, dsn)
}

func connectFirst(pgPort int) (*pgxpool.Pool, error) {
	var last error
	for _, name := range []string{"postgres", "wallet"} {
		dsn := fmt.Sprintf("postgres://wallet@127.0.0.1:%d/%s?sslmode=disable", pgPort, name)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		pool, err := pgxpool.New(ctx, dsn)
		if err != nil {
			cancel()
			last = err
			continue
		}
		err = pool.Ping(ctx)
		cancel()
		if err != nil {
			pool.Close()
			last = err
			continue
		}
		return pool, nil
	}
	if last == nil {
		last = errors.New("no connection attempt")
	}
	return nil, fmt.Errorf("connect to temporary postgres: %w", last)
}

func cloneTemplate(t *testing.T) string {
	t.Helper()
	name := "t" + strings.ReplaceAll(uuid.NewString(), "-", "")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		_, err = admin.Exec(ctx, "CREATE DATABASE "+name+" TEMPLATE wallet_template")
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	dsn := fmt.Sprintf("postgres://wallet@127.0.0.1:%d/%s?sslmode=disable", port, name)
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dropCancel()
		_, _ = admin.Exec(dropCtx, `
			SELECT pg_terminate_backend(pid)
			FROM pg_stat_activity
			WHERE datname = $1 AND pid <> pg_backend_pid()
		`, name)
		_, _ = admin.Exec(dropCtx, "DROP DATABASE IF EXISTS "+name)
	})
	return dsn
}

func truncateShared(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := admin.Exec(ctx, "TRUNCATE TABLE ledger_entries, idempotency_records, transfers, wallets RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("truncate database: %v", err)
	}
}

func migrate(ctx context.Context, dsn string) error {
	if err := postgres.Migrate(ctx, dsn); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

func postgresBinDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("WALLET_POSTGRES_BIN")); dir != "" {
		return dir, nil
	}
	if path, err := exec.LookPath("initdb"); err == nil {
		return filepath.Dir(path), nil
	}
	candidates := []string{
		"/opt/homebrew/opt/postgresql@16/bin",
		"/opt/homebrew/opt/postgresql@15/bin",
		"/opt/homebrew/opt/postgresql@14/bin",
		"/usr/local/opt/postgresql@16/bin",
		"/usr/local/opt/postgresql@15/bin",
		"/usr/lib/postgresql/16/bin",
		"/usr/lib/postgresql/15/bin",
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(filepath.Join(candidate, "initdb")); err == nil {
			return candidate, nil
		}
	}
	return "", errors.New("postgres binaries not found: set WALLET_POSTGRES_BIN or WALLET_TEST_DATABASE_URL")
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = listener.Close() }()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, output)
	}
	return nil
}

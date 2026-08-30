package testutil

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/codejk-crypto/wallet-transfer-assignment/internal/migrate"
)

var (
	startOnce sync.Once
	pool      *pgxpool.Pool
	startErr  error
	embedded  *embeddedpostgres.EmbeddedPostgres
)

func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	startOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		url := os.Getenv("TEST_DATABASE_URL")
		if url == "" {
			url, startErr = startEmbedded()
			if startErr != nil {
				return
			}
		}
		pool, startErr = pgxpool.New(ctx, url)
		if startErr != nil {
			return
		}
		startErr = migrate.Up(ctx, pool)
	})
	if startErr != nil {
		t.Fatalf("postgres not available: %v", startErr)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = pool.Exec(ctx, `TRUNCATE ledger_entries, idempotency_records, transfers, wallets CASCADE`)
	})
	return pool
}

func startEmbedded() (string, error) {
	port, err := freePort()
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "wallet-pg-*")
	if err != nil {
		return "", err
	}
	embedded = embeddedpostgres.NewDatabase(
		embeddedpostgres.DefaultConfig().
			Username("wallet").
			Password("wallet").
			Database("wallet").
			Port(uint32(port)).
			RuntimePath(dir).
			Logger(io.Discard),
	)
	if err := embedded.Start(); err != nil {
		return "", fmt.Errorf("embedded postgres: %w", err)
	}
	return fmt.Sprintf("postgres://wallet:wallet@127.0.0.1:%d/wallet?sslmode=disable", port), nil
}

func freePort() (uint32, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return uint32(l.Addr().(*net.TCPAddr).Port), nil
}

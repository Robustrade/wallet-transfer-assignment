//go:build integration

package service

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	containerpg "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var postgresContainer struct {
	once      sync.Once
	container *containerpg.PostgresContainer
	dsn       string
	err       error
}

// Startup is shared, but schemas, migrations, seed data, and pools are per test.
// Startup is lazy so filtered unit tests do not needlessly start PostgreSQL.
func containerDSN(t *testing.T) string {
	t.Helper()
	postgresContainer.once.Do(func() {
		// Some Docker discovery failures panic in this Testcontainers version.
		defer func() {
			if recovered := recover(); recovered != nil {
				postgresContainer.err = fmt.Errorf("Docker discovery: %v", recovered)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		postgresContainer.container, postgresContainer.err = containerpg.RunContainer(ctx,
			testcontainers.WithImage("postgres:16.3-alpine"),
			containerpg.WithDatabase("wallet_tests"), containerpg.WithUsername("wallet_test"), containerpg.WithPassword("test-only-password"),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(2*time.Minute),
				wait.ForListeningPort("5432/tcp"),
			),
		)
		if postgresContainer.err == nil {
			postgresContainer.dsn, postgresContainer.err = postgresContainer.container.ConnectionString(ctx, "sslmode=disable")
		}
	})
	if postgresContainer.err != nil {
		t.Fatalf("start Testcontainers PostgreSQL (requires a running Docker-compatible runtime): %v", postgresContainer.err)
	}
	return postgresContainer.dsn
}

func TestMain(m *testing.M) {
	code := m.Run()
	if postgresContainer.container != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := postgresContainer.container.Terminate(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "terminate PostgreSQL container: %v\n", err)
			code = 1
		}
		cancel()
	}
	os.Exit(code)
}

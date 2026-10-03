package apitest

import (
	"bytes"
	"context"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pravinkanna/wallet-transfer-assignment/internal/handler"
	"github.com/pravinkanna/wallet-transfer-assignment/internal/repository"
	"github.com/pravinkanna/wallet-transfer-assignment/internal/service"
)

// Shared by every test: one embedded Postgres, one pool, and one API server.
var (
	databaseURL string
	pool        *pgxpool.Pool
	apiURL      string
)

// discardLogger drops the log output of servers whose logs no test checks.
var discardLogger = slog.New(slog.DiscardHandler)

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	ctx := context.Background()

	dir, err := os.MkdirTemp("", "wallet-apitest-")
	if err != nil {
		log.Printf("create temp dir: %v", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()

	port, err := freePort()
	if err != nil {
		log.Printf("find free port: %v", err)
		return 1
	}

	home, err := os.UserHomeDir()
	if err != nil {
		log.Printf("find home directory: %v", err)
		return 1
	}

	var pgLog bytes.Buffer
	config := embeddedpostgres.DefaultConfig().
		Version(embeddedpostgres.V16).
		Port(port).
		RuntimePath(filepath.Join(dir, "runtime")).
		DataPath(filepath.Join(dir, "data")).
		// Unpacking the binaries is slow, so keep them next to the library's
		// download cache instead of in the per-run runtime directory.
		BinariesPath(filepath.Join(home, ".embedded-postgres-go", "binaries-"+string(embeddedpostgres.V16))).
		Logger(&pgLog)
	pg := embeddedpostgres.NewDatabase(config)
	if err := pg.Start(); err != nil {
		log.Printf("start embedded postgres: %v\n%s", err, pgLog.String())
		return 1
	}
	defer func() {
		if err := pg.Stop(); err != nil {
			log.Printf("stop embedded postgres: %v", err)
		}
	}()

	databaseURL = config.GetConnectionURL()
	pool, err = pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Printf("create pool: %v", err)
		return 1
	}
	defer pool.Close()

	if err := repository.ApplySchema(ctx, pool); err != nil {
		log.Print(err)
		return 1
	}

	server := httptest.NewServer(newHandler(pool, discardLogger))
	defer server.Close()
	apiURL = server.URL

	return m.Run()
}

// newHandler builds the API on pool, wired the same way as in cmd/server.
func newHandler(pool *pgxpool.Pool, logger *slog.Logger) http.Handler {
	return handler.New(service.New(repository.New(pool)), logger)
}

// freePort asks the OS for an unused TCP port.
func freePort() (uint32, error) {
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return uint32(l.Addr().(*net.TCPAddr).Port), nil
}

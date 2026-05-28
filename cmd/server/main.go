// Command server is the entrypoint binary.
//
// The package is intentionally tiny: a `main` that sets up logging
// and signal handling, and a `run` that wires the application and
// serves HTTP until cancelled. Splitting the two lets integration
// tests drive `run` with a context they control while keeping `main`
// limited to the things only a binary entrypoint needs to do
// (slog default handler, os.Exit, signal subscription).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/PratikDhanave/wallet-transfer-assignment/internal/config"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/db"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/handler"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/metrics"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/service"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/tracing"
)

// main wires only the things that are inherently process-level:
// logger setup, config load, signal subscription, and the os.Exit
// on fatal error. Everything else is in run.
func main() {
	// JSON to stdout is the right default for containerised
	// deployment — log aggregators (Loki, Datadog, CloudWatch, …)
	// parse JSON natively and the structured fields stay queryable.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Load config before opening the signal context, so a bad DSN
	// fails fast without registering a no-op handler.
	cfg, err := config.Load()
	if err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}

	// signal.NotifyContext returns a context that is cancelled when
	// the process receives SIGINT (Ctrl-C) or SIGTERM (the standard
	// Kubernetes / systemd shutdown signal). Passing it into run
	// lets the same code path serve "the user pressed Ctrl-C" and
	// "the orchestrator wants us to stop" without branching.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

// run wires the application and serves HTTP until ctx is cancelled
// or the listener errors. It is split out from main so integration
// tests can drive it with a context they control.
//
// Lifecycle:
//
//  1. Open the database and apply pending migrations. A failure here
//     means the service has no business serving traffic, so we return
//     the error and let main bail out.
//
//  2. Build the repositories, services, and HTTP handler tree.
//
//  3. Optionally start the pprof admin listener on cfg.DebugAddr
//     (only if it's non-empty). The listener runs on its own goroutine
//     with its own *http.Server so its lifecycle is independent from
//     the API listener.
//
//  4. Start the public API listener on cfg.HTTPAddr in another
//     goroutine. Any non-graceful error from ListenAndServe surfaces
//     via errCh and triggers an early return.
//
//  5. Block on ctx.Done() or errCh. Whichever fires first, we move
//     on to graceful shutdown with a 10-second deadline.
//
//  6. Shut down both servers. Shutdown stops accepting new
//     connections and waits up to the deadline for existing
//     requests to finish.
func run(ctx context.Context, cfg config.Config) error {
	// 0. Tracing. Spans go to stderr via the stdout exporter so a
	// reviewer running `make run` sees them without setting up a
	// collector. The shutdown function flushes buffered spans on
	// exit; without it, spans in flight at shutdown are dropped.
	shutdownTracing, err := tracing.Setup(ctx, os.Stderr)
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = shutdownTracing(shutdownCtx)
	}()

	// 1. Database + migrations.
	database, err := db.OpenWithPool(cfg.DatabaseURL, db.PoolConfig{
		MaxOpenConns:    cfg.DBMaxOpenConns,
		MaxIdleConns:    cfg.DBMaxIdleConns,
		ConnMaxLifetime: cfg.DBConnMaxLifetime,
		ConnMaxIdleTime: cfg.DBConnMaxIdleTime,
	})
	if err != nil {
		return err
	}
	defer database.Close()
	slog.Info("db pool configured",
		"max_open", cfg.DBMaxOpenConns,
		"max_idle", cfg.DBMaxIdleConns,
		"conn_max_lifetime", cfg.DBConnMaxLifetime,
		"conn_max_idle_time", cfg.DBConnMaxIdleTime,
	)

	if err := db.Migrate(database); err != nil {
		return err
	}

	// 2. Repositories (stateless), services (hold dependencies),
	// and handlers (hold services). Wiring done once at startup;
	// per-request work re-uses these pointers.
	walletRepo := postgres.NewWalletRepo()
	transferRepo := postgres.NewTransferRepo()
	ledgerRepo := postgres.NewLedgerRepo()
	idemRepo := postgres.NewIdempotencyRepo()
	txm := postgres.NewTxManager(database)

	transferSvc := service.NewTransferService(database, txm, walletRepo, transferRepo, ledgerRepo, idemRepo)
	walletSvc := service.NewWalletService(database, walletRepo)

	apiMux := handler.NewRouter(
		handler.NewTransferHandler(transferSvc),
		handler.NewWalletHandler(walletSvc, transferSvc),
	)

	// Stack additional concerns OUTSIDE the route mux:
	//   - metrics.HTTPMiddleware records Prometheus counters /
	//     histograms per request (route + method + status).
	//   - otelhttp.NewHandler wraps the whole thing so every HTTP
	//     request becomes a parent span with the standard semantic
	//     conventions populated.
	apiHandler := otelhttp.NewHandler(
		metrics.HTTPMiddleware(apiMux),
		"api",
	)

	srv := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: apiHandler,
		// ReadHeaderTimeout protects against slowloris-style attacks
		// where a client never finishes sending headers and holds a
		// goroutine open. 5 seconds is comfortably long for any
		// honest client and fatal for any attacker.
		ReadHeaderTimeout: 5 * time.Second,
	}

	// 3. Optional pprof admin listener. See AGENTS.md §S-16:
	// pprof must never share the public API port.
	var debugSrv *http.Server
	if cfg.DebugAddr != "" {
		if !isLoopbackAddr(cfg.DebugAddr) {
			// Non-loopback exposure is a real risk, not just style.
			// We refuse to silently allow it: emit a WARN at startup
			// so a human reading the logs immediately sees the
			// security implication.
			slog.Warn("debug listener bound to non-loopback address; pprof exposes runtime internals",
				"addr", cfg.DebugAddr)
		}
		debugSrv = &http.Server{
			Addr:              cfg.DebugAddr,
			Handler:           handler.NewDebugMux(),
			ReadHeaderTimeout: 5 * time.Second,
		}
		go func() {
			slog.Info("debug listener up", "addr", cfg.DebugAddr)
			if err := debugSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("debug listener failed", "error", err)
			}
		}()
	}

	// 4. Public API listener on its own goroutine. We use a
	// buffered channel of size 1 so the goroutine never blocks
	// when it sends the error (in case the main select has
	// already moved on to a context-cancel path).
	errCh := make(chan error, 1)
	go func() {
		slog.Info("server listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	// 5. Wait for either a shutdown signal or a listener error.
	select {
	case <-ctx.Done():
		slog.Info("shutdown signal received")
	case err := <-errCh:
		return err
	}

	// 6. Graceful shutdown. 10s is a typical Kubernetes-friendly
	// value — long enough for in-flight requests to drain, short
	// enough that a crashed handler can't pin the rollout.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if debugSrv != nil {
		_ = debugSrv.Shutdown(shutdownCtx)
	}
	return srv.Shutdown(shutdownCtx)
}

// isLoopbackAddr returns true when addr's host portion is a loopback
// address (localhost / 127.0.0.1 / ::1). An empty host (e.g. ":6060")
// binds 0.0.0.0 and is treated as NON-loopback — that's the case the
// WARN in run() is specifically guarding against.
//
// Used only to decide whether to warn about pprof exposure; the
// listener itself binds to whatever the operator configured.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

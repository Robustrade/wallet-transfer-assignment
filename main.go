package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sync/errgroup"
	"wallet-transfer/internal/config"
	"wallet-transfer/internal/db"
	"wallet-transfer/internal/handler"
	"wallet-transfer/internal/logger"
	"wallet-transfer/internal/observability/metrics"
	"wallet-transfer/internal/observability/tracing"
	"wallet-transfer/internal/repository/postgres"
	"wallet-transfer/internal/service"
	"wallet-transfer/internal/worker"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

// run wires dependencies (manual constructor injection) and blocks until the
// process receives SIGINT/SIGTERM, then shuts down gracefully.
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	log, err := logger.New(cfg.LogLevel)
	if err != nil {
		return fmt.Errorf("build logger: %w", err)
	}
	defer func() { _ = log.Sync() }()

	// Cancelled on the first interrupt/terminate signal.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tracingShutdown, err := tracing.Init(ctx, tracing.Config{
		Enabled:     cfg.TracingEnabled,
		Endpoint:    cfg.OTLPEndpoint,
		ServiceName: cfg.ServiceName,
		SampleRatio: cfg.TraceSampleRatio,
	})
	if err != nil {
		return fmt.Errorf("init tracing: %w", err)
	}
	defer func() { _ = tracingShutdown(context.Background()) }()

	pool, err := db.NewPool(ctx, cfg)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()

	m := metrics.New()
	m.RegisterDBPool(pool)

	store := postgres.New(pool)
	transfers := service.NewTransferService(store, log, m)
	processor := service.NewProcessor(store, log, cfg.MaxAttempts, m)

	srv := handler.New(cfg, log, pool, transfers, m)
	wrk := worker.New(store, processor, log, worker.Config{
		Count:        cfg.WorkerCount,
		BatchSize:    cfg.WorkerBatchSize,
		PollInterval: cfg.WorkerPollInterval,
		ReapInterval: cfg.ReaperInterval,
		StuckAfter:   cfg.StuckAfter,
	})

	// Run the HTTP server and the async worker together; a signal (ctx cancel)
	// or a fatal error in either stops both, then each drains gracefully.
	log.Info("dependencies ready, starting server and worker")
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return srv.Run(gctx) })
	g.Go(func() error { return wrk.Run(gctx) })
	return g.Wait()
}

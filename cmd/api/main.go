package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/ankit729/wallet-transfer/internal/cache"
	"github.com/ankit729/wallet-transfer/internal/httpapi"
	"github.com/ankit729/wallet-transfer/internal/queue"
	"github.com/ankit729/wallet-transfer/internal/repository"
	"github.com/ankit729/wallet-transfer/internal/service"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	logger := slog.New(
		slog.NewJSONHandler(os.Stdout, nil),
	)
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("application stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	httpAddr := envOrDefault("HTTP_ADDR", ":8080")
	databaseURL := envOrDefault(
		"DATABASE_URL",
		"postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable",
	)
	redisURL := envOrDefault(
		"REDIS_URL",
		"redis://localhost:6379/0",
	)

	maxConns := int32(10)
	if raw := os.Getenv("DB_MAX_CONNS"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			return err
		}

		maxConns = int32(parsed)
	}

	cacheTTL := 30 * time.Second
	if raw := os.Getenv("CACHE_TTL"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return err
		}

		cacheTTL = parsed
	}

	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return err
	}

	poolConfig.MaxConns = maxConns

	pool, err := pgxpool.NewWithConfig(
		context.Background(),
		poolConfig,
	)
	if err != nil {
		return err
	}
	defer pool.Close()

	pingContext, cancelPing := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancelPing()

	if err := pool.Ping(pingContext); err != nil {
		return err
	}

	var walletCache *cache.RedisCache

	redisContext, cancelRedis := context.WithTimeout(
		context.Background(),
		3*time.Second,
	)
	walletCache, err = cache.New(
		redisContext,
		redisURL,
		cacheTTL,
	)
	cancelRedis()

	if err != nil {
		slog.Warn(
			"redis unavailable; continuing without cache",
			"error", err,
		)
		walletCache = nil
	} else {
		defer walletCache.Close()
	}

	store := repository.New(pool)
	eventQueue := queue.NewMemory(1024)
	applicationService := service.New(store, walletCache, eventQueue)
	handler := httpapi.New(applicationService)

	server := &http.Server{
		Addr:              httpAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErrors := make(chan error, 1)

	go func() {
		slog.Info("http server started", "addr", httpAddr)
		serverErrors <- server.ListenAndServe()
	}()

	signalContext, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()
	eventQueue.Start(signalContext, applicationService.ProcessEvent)

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return err

	case <-signalContext.Done():
		shutdownContext, cancelShutdown := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)
		defer cancelShutdown()

		return server.Shutdown(shutdownContext)
	}
}

func envOrDefault(
	key string,
	fallback string,
) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}

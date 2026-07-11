// Command server wires config, Postgres, Redis, migrations, and the HTTP
// router, then serves the wallet transfer API until a SIGTERM/SIGINT
// triggers a graceful shutdown.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/config"
	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/handler"
	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/idgen"
	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/platform/logger"
	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/platform/migrate"
	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/platform/postgres"
	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/platform/redisclient"
	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/repository/cache"
	pgrepo "github.com/Nandukrishna-S/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/service"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	migrationsPath  = "migrations"
	pingTimeout     = 5 * time.Second
	shutdownTimeout = 10 * time.Second
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", slog.Any("error", err))
		os.Exit(1)
	}

	if err := cfg.Validate(); err != nil {
		slog.Error("invalid config", slog.Any("error", err))
		os.Exit(1)
	}

	log := logger.New()

	ctx := context.Background()

	pool, err := postgres.New(ctx, cfg.PostgresDSN)
	if err != nil {
		log.Error("create postgres pool", slog.Any("error", err))
		os.Exit(1)
	}
	defer pool.Close()

	if err := pingPostgres(ctx, pool); err != nil {
		log.Error("ping postgres", slog.Any("error", err))
		os.Exit(1)
	}

	if err := migrate.Run(cfg.PostgresDSN, migrationsPath); err != nil {
		log.Error("run migrations", slog.Any("error", err))
		os.Exit(1)
	}

	redisClient := redisclient.New(cfg.RedisAddr)
	defer func() {
		if err := redisClient.Close(); err != nil {
			log.Error("close redis client", slog.Any("error", err))
		}
	}()

	if err := pingRedis(ctx, redisClient); err != nil {
		log.Error("ping redis", slog.Any("error", err))
		os.Exit(1)
	}

	generator, err := idgen.NewGenerator(cfg.WorkerID)
	if err != nil {
		log.Error("create id generator", slog.Any("error", err))
		os.Exit(1)
	}

	transferService := service.NewTransferService(
		pgrepo.NewTransactor(pool),
		pgrepo.NewWalletRepo(),
		pgrepo.NewTransferRepo(pool),
		cache.NewTransferCache(redisClient),
		generator,
		cfg.LockTimeout,
		log,
	)

	healthHandler := handler.NewHealthHandler(pool, redisClient)
	transferHandler := handler.NewTransferHandler(transferService, log)
	router := handler.NewRouter(healthHandler, transferHandler)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	runServer(log, srv)
}

func pingPostgres(ctx context.Context, pool *pgxpool.Pool) error {
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	return pool.Ping(ctx)
}

func pingRedis(ctx context.Context, client *redis.Client) error {
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	return client.Ping(ctx).Err()
}

func runServer(log *slog.Logger, srv *http.Server) {
	go func() {
		log.Info("starting server", slog.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server error", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	<-stop

	log.Info("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Error("graceful shutdown failed", slog.Any("error", err))
	}
}

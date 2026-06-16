package main

// @title           Wallet Transfer API
// @version         1.0
// @description     Wallet-to-wallet transfer service with idempotency, double-entry ledger, and PostgreSQL.
// @termsOfService  http://swagger.io/terms/

// @contact.name   API Support

// @license.name  MIT
// @license.url   https://opensource.org/licenses/MIT

// @host      localhost:8080
// @BasePath  /

// @schemes   http
import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/synchone/wallet-transfer-assignment/internal/config"
	_ "github.com/synchone/wallet-transfer-assignment/docs"
	"github.com/synchone/wallet-transfer-assignment/internal/handler"
	appmigrate "github.com/synchone/wallet-transfer-assignment/internal/migrate"
	"github.com/synchone/wallet-transfer-assignment/internal/observability"
	"github.com/synchone/wallet-transfer-assignment/internal/repository"
	"github.com/synchone/wallet-transfer-assignment/internal/service"
)

func main() {
	gin.SetMode(gin.ReleaseMode)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	logger := observability.NewLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	ctx := context.Background()
	if err := appmigrate.Up(cfg.DatabaseURL); err != nil {
		logger.Error("run migrations", "error", err)
		os.Exit(1)
	}

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("connect database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	repo := repository.New(pool)
	router := handler.NewRouter(handler.Dependencies{
		TransferService: service.NewTransferService(repo, logger),
		HealthPinger:    handler.NewHealthPinger(repo),
		WalletBalances:  handler.NewWalletBalanceService(repo),
		Logger:          logger,
	})

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("server starting", "port", cfg.HTTPPort)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "error", err)
	}
	logger.Info("server stopped")
}

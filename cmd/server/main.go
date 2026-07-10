package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/loopinnovates/wallet-transfer-assignment/internal/config"
	"github.com/loopinnovates/wallet-transfer-assignment/internal/router"
	"github.com/loopinnovates/wallet-transfer-assignment/internal/service"
	"github.com/loopinnovates/wallet-transfer-assignment/internal/worker"
	"github.com/loopinnovates/wallet-transfer-assignment/pkg/factory"
	"github.com/loopinnovates/wallet-transfer-assignment/pkg/logger"
	"github.com/loopinnovates/wallet-transfer-assignment/repository/postgres"
)

const shutdownTimeout = 10 * time.Second

func main() {
	appConfig := config.AppConfig()
	logger.Init(appConfig.LogLevel)

	appFactory, err := factory.NewFactory(appConfig)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer appFactory.Close()

	transferRepo := postgres.NewTransferRepository(appFactory.ReadPGReplica, appFactory.WritePGReplica)

	RouterContext := &router.RouterContext{
		WalletSvc: &service.WalletService{
			WalletRepo:   postgres.NewWalletRepository(appFactory.ReadPGReplica, appFactory.WritePGReplica),
			TransferRepo: transferRepo,
		},
		MaxConcurrentRequests: appConfig.MaxConcurrentRequests,
	}

	srv := &http.Server{
		Addr:    ":" + appConfig.Port,
		Handler: router.New(RouterContext),
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info().Str("port", appConfig.Port).Msg("listening")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	reconCtx, cancelRecon := context.WithCancel(context.Background())
	defer cancelRecon()
	recon := &worker.PendingTransferRecon{TransferRepo: transferRepo}
	go recon.Run(reconCtx)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serveErr:
		cancelRecon()
		if err != nil {
			logger.Fatal().Err(err).Msg("server failed")
		}
	case sig := <-stop:
		logger.Info().Str("signal", sig.String()).Msg("received signal, shutting down gracefully")
		cancelRecon()

		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			logger.Fatal().Err(err).Msg("graceful shutdown failed")
		}
		logger.Info().Msg("server shut down cleanly")
	}
}

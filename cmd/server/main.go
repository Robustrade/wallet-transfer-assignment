package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/config"
	"github.com/Robustrade/wallet-transfer-assignment/handler"
	"github.com/Robustrade/wallet-transfer-assignment/repository"
	"github.com/Robustrade/wallet-transfer-assignment/service"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	config.LoadDotEnv()
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	pool, err := newPool(ctx, cfg)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	if err := repository.Migrate(ctx, pool, cfg.MigrationsDir); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	svc := service.New(pool)
	h := handler.New(svc)
	mux := http.NewServeMux()
	h.Routes(mux)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler.WithRequestTimeout(cfg.HTTPRequestTimeout, mux),
		ReadHeaderTimeout: cfg.HTTPReadHeaderTimeout,
		ReadTimeout:       cfg.HTTPReadTimeout,
		WriteTimeout:      cfg.HTTPWriteTimeout,
		IdleTimeout:       cfg.HTTPIdleTimeout,
	}
	go func() {
		log.Printf("listening on %s (db pool max_conns=%d min_conns=%d request_timeout=%s)",
			cfg.HTTPAddr, cfg.DBPoolMaxConns, cfg.DBPoolMinConns, cfg.HTTPRequestTimeout)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

func newPool(ctx context.Context, cfg config.Config) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	poolCfg.MaxConns = cfg.DBPoolMaxConns
	poolCfg.MinConns = cfg.DBPoolMinConns
	return pgxpool.NewWithConfig(ctx, poolCfg)
}

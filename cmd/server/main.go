package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/avinilcodes/wallet-transfer-assignment/internal/config"
	"github.com/avinilcodes/wallet-transfer-assignment/internal/handler"
	"github.com/avinilcodes/wallet-transfer-assignment/internal/platform/postgres"
	repoPostgres "github.com/avinilcodes/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/avinilcodes/wallet-transfer-assignment/internal/service"
)

func main() {
	cfg := config.LoadDatabase()

	db, err := postgres.Open(cfg.DSN())
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer db.Close()

	store := repoPostgres.NewStore(db)
	transferService := service.NewTransferService(store)

	router := handler.NewRouter(
		handler.NewTransferHandler(transferService),
		handler.NewHealthHandler(),
	)

	addr := envOrDefault("HTTP_ADDR", ":8080")
	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	go func() {
		log.Printf("listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

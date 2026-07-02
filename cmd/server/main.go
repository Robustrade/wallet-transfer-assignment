package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/priiyank-xd/wallet-transfer-service/internal/config"
	"github.com/priiyank-xd/wallet-transfer-service/internal/handler"
	"github.com/priiyank-xd/wallet-transfer-service/internal/repository"
	"github.com/priiyank-xd/wallet-transfer-service/internal/service"
)

func main() {
	//load config
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	//postgres connection
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Fatal("database ping failed: ", err)
	}

	///handler
	repo := repository.New(pool)
	svc := service.NewTransferService(repo)
	h := handler.NewTransferHandler(svc)

	// routs
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("wallet-transfer-service"))
	})
	h.RegisterRoutes(mux)

	// server
	srv := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: mux,
	}

	go func() {
		log.Printf("listening on %s", cfg.HTTPAddr)
		err := srv.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	// graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatal("server shutdown failed: ", err)
	}
}

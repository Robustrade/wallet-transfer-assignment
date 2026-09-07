package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"Wallet_Transfer_Service/internal/handler"
	"Wallet_Transfer_Service/internal/repository"
	"Wallet_Transfer_Service/internal/repository/memory"
	"Wallet_Transfer_Service/internal/repository/postgres"
	"Wallet_Transfer_Service/internal/service"
)

func main() {
	store, closeStore, err := buildStore()
	if err != nil {
		log.Fatalf("build store: %v", err)
	}
	defer closeStore()

	transferSvc := service.NewTransferService(store)
	walletSvc := service.NewWalletService(store)
	router := handler.NewRouter(transferSvc, walletSvc)

	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("wallet transfer service listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

func buildStore() (repository.Store, func(), error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Print("DATABASE_URL not set, using in-memory store (dev/demo mode)")
		return memory.NewStore(), func() {}, nil
	}

	db, err := postgres.Open(dsn)
	if err != nil {
		return nil, nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, nil, err
	}
	log.Print("connected to Postgres")
	return postgres.NewStore(db), func() { db.Close() }, nil
}

package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"

	"github.com/ankurnitt1311/wallet-transfer-assignment/internal/domain"
	"github.com/ankurnitt1311/wallet-transfer-assignment/internal/handler"
	"github.com/ankurnitt1311/wallet-transfer-assignment/internal/repository"
	"github.com/ankurnitt1311/wallet-transfer-assignment/internal/service"
)

func main() {
	if err := os.MkdirAll("data", 0o755); err != nil {
		log.Fatal(err)
	}
	store, err := repository.OpenConcurrent(repository.FileDSN("data/wallet.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(); err != nil {
		log.Fatal(err)
	}

	svc := service.New(store, nil)
	ctx := context.Background()
	// Seed the wallets used in ASSIGNMENT.md so POST /transfers can be exercised.
	if err := ensureWallet(ctx, svc, "wallet_1", 1000); err != nil {
		log.Fatal(err)
	}
	if err := ensureWallet(ctx, svc, "wallet_2", 0); err != nil {
		log.Fatal(err)
	}

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", handler.New(svc).Routes()))
}

func ensureWallet(ctx context.Context, svc *service.Service, id string, balance int64) error {
	_, err := svc.CreateWallet(ctx, id, balance)
	if err == nil || errors.Is(err, domain.ErrWalletExists) {
		return nil
	}
	return err
}

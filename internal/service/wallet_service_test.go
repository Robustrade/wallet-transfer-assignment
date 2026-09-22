package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/wallet-transfer-assignment/wallet-transfer/internal/domain"
	"github.com/wallet-transfer-assignment/wallet-transfer/internal/service"
	"github.com/wallet-transfer-assignment/wallet-transfer/internal/testutil"
)

func TestWalletService_CreateAndReadWallet(t *testing.T) {
	uow := testutil.NewMockUnitOfWork()
	wallets := service.NewWalletService(uow)
	ctx := context.Background()

	created, err := wallets.CreateWallet(ctx, service.CreateWalletInput{InitialBalance: 25})
	if err != nil {
		t.Fatalf("CreateWallet: %v", err)
	}
	if created.ID == "" || created.Balance != 25 {
		t.Fatalf("created wallet = %#v", created)
	}

	found, err := wallets.GetWallet(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetWallet: %v", err)
	}
	if found.Balance != 25 {
		t.Fatalf("balance = %d, want 25", found.Balance)
	}

	history, err := wallets.ListTransferHistory(ctx, created.ID)
	if err != nil {
		t.Fatalf("ListTransferHistory: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("history length = %d, want 0", len(history))
	}
}

func TestWalletService_RejectsInvalidAndDuplicateWallets(t *testing.T) {
	uow := testutil.NewMockUnitOfWork()
	wallets := service.NewWalletService(uow)
	ctx := context.Background()

	if _, err := wallets.CreateWallet(ctx, service.CreateWalletInput{InitialBalance: -1}); !errors.Is(err, domain.ErrInvalidAmount) {
		t.Fatalf("negative balance error = %v, want ErrInvalidAmount", err)
	}

	if _, err := wallets.CreateWallet(ctx, service.CreateWalletInput{ID: "wallet-1", InitialBalance: 1}); err != nil {
		t.Fatalf("first wallet: %v", err)
	}
	if _, err := wallets.CreateWallet(ctx, service.CreateWalletInput{ID: "wallet-1", InitialBalance: 1}); !errors.Is(err, domain.ErrWalletAlreadyExists) {
		t.Fatalf("duplicate wallet error = %v, want ErrWalletAlreadyExists", err)
	}
	if _, err := wallets.GetWallet(ctx, "missing"); !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("missing wallet error = %v, want ErrWalletNotFound", err)
	}
	if _, err := wallets.ListTransferHistory(ctx, "missing"); !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("missing history error = %v, want ErrWalletNotFound", err)
	}
}

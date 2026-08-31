package service_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mshabab1296/wallet-transfer-assignment/internal/domain"
	"github.com/mshabab1296/wallet-transfer-assignment/internal/repository"
	"github.com/mshabab1296/wallet-transfer-assignment/internal/service"
)

func newService(t *testing.T, balance int64) (*service.TransferService, *repository.SQLiteRepository) {
	t.Helper()
	repo, err := repository.OpenSQLite(filepath.Join(t.TempDir(), "wallets.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	for id, amount := range map[string]int64{"alice": balance, "bob": 0, "carol": 0} {
		if err := repo.CreateWallet(context.Background(), id, amount); err != nil {
			t.Fatal(err)
		}
	}
	return service.NewTransferService(repo), repo
}
func TestTransferUpdatesBalancesAndCreatesBalancedPair(t *testing.T) {
	svc, repo := newService(t, 100)
	got, err := svc.CreateTransfer(context.Background(), domain.CreateTransferRequest{IdempotencyKey: "one", FromWalletID: "alice", ToWalletID: "bob", Amount: 40})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.Processed {
		t.Fatalf("status=%s", got.Status)
	}
	if n, err := repo.LedgerEntryCount(context.Background(), got.ID); err != nil || n != 2 {
		t.Fatalf("entries=%d err=%v", n, err)
	}
	if balance, _ := repo.Balance(context.Background(), "alice"); balance != 60 {
		t.Fatalf("alice=%d", balance)
	}
	if balance, _ := repo.Balance(context.Background(), "bob"); balance != 40 {
		t.Fatalf("bob=%d", balance)
	}
}
func TestSameIdempotencyKeyReplaysOriginalTransfer(t *testing.T) {
	svc, repo := newService(t, 100)
	request := domain.CreateTransferRequest{IdempotencyKey: "same", FromWalletID: "alice", ToWalletID: "bob", Amount: 40}
	first, err := svc.CreateTransfer(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateTransfer(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || !second.Replayed {
		t.Fatalf("expected replay, got %#v", second)
	}
	if balance, _ := repo.Balance(context.Background(), "alice"); balance != 60 {
		t.Fatalf("duplicate debited twice: %d", balance)
	}
	if n, _ := repo.LedgerEntryCount(context.Background(), first.ID); n != 2 {
		t.Fatalf("entries=%d", n)
	}
}
func TestKeyReuseWithDifferentRequestIsRejected(t *testing.T) {
	svc, _ := newService(t, 100)
	_, err := svc.CreateTransfer(context.Background(), domain.CreateTransferRequest{IdempotencyKey: "same", FromWalletID: "alice", ToWalletID: "bob", Amount: 40})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateTransfer(context.Background(), domain.CreateTransferRequest{IdempotencyKey: "same", FromWalletID: "alice", ToWalletID: "carol", Amount: 40})
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("err=%v", err)
	}
}
func TestInsufficientFundsIsRecordedAndReplaySafe(t *testing.T) {
	svc, repo := newService(t, 10)
	request := domain.CreateTransferRequest{IdempotencyKey: "too-much", FromWalletID: "alice", ToWalletID: "bob", Amount: 20}
	first, err := svc.CreateTransfer(context.Background(), request)
	if !errors.Is(err, domain.ErrInsufficientFunds) || first.Status != domain.Failed {
		t.Fatalf("transfer=%#v err=%v", first, err)
	}
	second, err := svc.CreateTransfer(context.Background(), request)
	if !errors.Is(err, domain.ErrInsufficientFunds) || !second.Replayed || second.ID != first.ID {
		t.Fatalf("transfer=%#v err=%v", second, err)
	}
	if balance, _ := repo.Balance(context.Background(), "alice"); balance != 10 {
		t.Fatalf("balance=%d", balance)
	}
	if n, _ := repo.LedgerEntryCount(context.Background(), first.ID); n != 0 {
		t.Fatalf("entries=%d", n)
	}
}
func TestConcurrentTransfersCannotDoubleSpend(t *testing.T) {
	svc, repo := newService(t, 100)
	requests := []domain.CreateTransferRequest{{IdempotencyKey: "one", FromWalletID: "alice", ToWalletID: "bob", Amount: 80}, {IdempotencyKey: "two", FromWalletID: "alice", ToWalletID: "carol", Amount: 80}}
	results := make(chan error, len(requests))
	var wg sync.WaitGroup
	for _, request := range requests {
		wg.Add(1)
		go func(req domain.CreateTransferRequest) {
			defer wg.Done()
			_, err := svc.CreateTransfer(context.Background(), req)
			results <- err
		}(request)
	}
	wg.Wait()
	close(results)
	processed, failed := 0, 0
	for err := range results {
		if err == nil {
			processed++
		} else if errors.Is(err, domain.ErrInsufficientFunds) {
			failed++
		} else {
			t.Fatal(err)
		}
	}
	if processed != 1 || failed != 1 {
		t.Fatalf("processed=%d failed=%d", processed, failed)
	}
	if balance, _ := repo.Balance(context.Background(), "alice"); balance != 20 {
		t.Fatalf("alice=%d", balance)
	}
}

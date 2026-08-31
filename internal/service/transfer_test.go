package service_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mshabab1296/wallet-transfer-assignment/internal/domain"
	"github.com/mshabab1296/wallet-transfer-assignment/internal/repository"
	"github.com/mshabab1296/wallet-transfer-assignment/internal/service"
)

var sequence atomic.Int64

type fixture struct {
	svc               *service.TransferService
	repo              *repository.PostgresRepository
	alice, bob, carol string
}

func newFixture(t *testing.T, balance int64) fixture {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL or run via docker compose to execute PostgreSQL integration tests")
	}
	repo, err := repository.OpenPostgres(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	prefix := fmt.Sprintf("test_%d_%d", time.Now().UnixNano(), sequence.Add(1))
	f := fixture{svc: service.NewTransferService(repo), repo: repo, alice: prefix + "_alice", bob: prefix + "_bob", carol: prefix + "_carol"}
	for id, amount := range map[string]int64{f.alice: balance, f.bob: 0, f.carol: 0} {
		if err := repo.CreateWallet(context.Background(), id, amount); err != nil {
			t.Fatal(err)
		}
	}
	return f
}
func TestTransferUpdatesBalancesAndCreatesBalancedPair(t *testing.T) {
	f := newFixture(t, 100)
	got, err := f.svc.CreateTransfer(context.Background(), domain.CreateTransferRequest{IdempotencyKey: f.alice + "_one", FromWalletID: f.alice, ToWalletID: f.bob, Amount: 40})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.Processed {
		t.Fatalf("status=%s", got.Status)
	}
	if n, err := f.repo.LedgerEntryCount(context.Background(), got.ID); err != nil || n != 2 {
		t.Fatalf("entries=%d err=%v", n, err)
	}
	if balance, _ := f.repo.Balance(context.Background(), f.alice); balance != 60 {
		t.Fatalf("alice=%d", balance)
	}
	if balance, _ := f.repo.Balance(context.Background(), f.bob); balance != 40 {
		t.Fatalf("bob=%d", balance)
	}
}
func TestSameIdempotencyKeyReplaysOriginalTransfer(t *testing.T) {
	f := newFixture(t, 100)
	request := domain.CreateTransferRequest{IdempotencyKey: f.alice + "_same", FromWalletID: f.alice, ToWalletID: f.bob, Amount: 40}
	first, err := f.svc.CreateTransfer(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.svc.CreateTransfer(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || !second.Replayed {
		t.Fatalf("expected replay, got %#v", second)
	}
	if balance, _ := f.repo.Balance(context.Background(), f.alice); balance != 60 {
		t.Fatalf("duplicate debited twice: %d", balance)
	}
}
func TestKeyReuseWithDifferentRequestIsRejected(t *testing.T) {
	f := newFixture(t, 100)
	_, err := f.svc.CreateTransfer(context.Background(), domain.CreateTransferRequest{IdempotencyKey: f.alice + "_same", FromWalletID: f.alice, ToWalletID: f.bob, Amount: 40})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.CreateTransfer(context.Background(), domain.CreateTransferRequest{IdempotencyKey: f.alice + "_same", FromWalletID: f.alice, ToWalletID: f.carol, Amount: 40})
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("err=%v", err)
	}
}
func TestInsufficientFundsIsRecordedAndReplaySafe(t *testing.T) {
	f := newFixture(t, 10)
	request := domain.CreateTransferRequest{IdempotencyKey: f.alice + "_too_much", FromWalletID: f.alice, ToWalletID: f.bob, Amount: 20}
	first, err := f.svc.CreateTransfer(context.Background(), request)
	if !errors.Is(err, domain.ErrInsufficientFunds) || first.Status != domain.Failed {
		t.Fatalf("transfer=%#v err=%v", first, err)
	}
	second, err := f.svc.CreateTransfer(context.Background(), request)
	if !errors.Is(err, domain.ErrInsufficientFunds) || !second.Replayed || second.ID != first.ID {
		t.Fatalf("transfer=%#v err=%v", second, err)
	}
	if balance, _ := f.repo.Balance(context.Background(), f.alice); balance != 10 {
		t.Fatalf("balance=%d", balance)
	}
}
func TestConcurrentTransfersCannotDoubleSpend(t *testing.T) {
	f := newFixture(t, 100)
	requests := []domain.CreateTransferRequest{{IdempotencyKey: f.alice + "_one", FromWalletID: f.alice, ToWalletID: f.bob, Amount: 80}, {IdempotencyKey: f.alice + "_two", FromWalletID: f.alice, ToWalletID: f.carol, Amount: 80}}
	results := make(chan error, len(requests))
	var wg sync.WaitGroup
	for _, request := range requests {
		wg.Add(1)
		go func(req domain.CreateTransferRequest) {
			defer wg.Done()
			_, err := f.svc.CreateTransfer(context.Background(), req)
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
	if balance, _ := f.repo.Balance(context.Background(), f.alice); balance != 20 {
		t.Fatalf("alice=%d", balance)
	}
}

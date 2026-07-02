package service

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/priiyank-xd/wallet-transfer-service/internal/domain"
	"github.com/priiyank-xd/wallet-transfer-service/internal/repository"
	"github.com/priiyank-xd/wallet-transfer-service/internal/testutil"
)

func newTestService(t *testing.T) (*TransferService, *pgxpool.Pool) {
	t.Helper()

	pool := testutil.Setup(t)
	return NewTransferService(repository.New(pool)), pool
}

func TestCreateTransferValidation(t *testing.T) {
	svc, _ := newTestService(t)

	_, status, err := svc.CreateTransfer(context.Background(), domain.TransferRequest{
		FromWalletID: testutil.WalletAlice,
		ToWalletID:   testutil.WalletBob,
		Amount:       100,
	})
	if !errors.Is(err, domain.ErrEmptyIdempotency) {
		t.Fatalf("expected ErrEmptyIdempotency, got %v", err)
	}
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}

func TestCreateTransferSuccess(t *testing.T) {
	svc, pool := newTestService(t)

	key := testutil.NewIdempotencyKey(t)
	req := domain.TransferRequest{
		IdempotencyKey: key,
		FromWalletID:   testutil.WalletAlice,
		ToWalletID:     testutil.WalletBob,
		Amount:         100,
	}

	resp, status, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d", status)
	}
	if resp.Status != domain.PROCESSED {
		t.Fatalf("expected PROCESSED, got %s", resp.Status)
	}
	if resp.TransferID == "" {
		t.Fatal("expected transfer id in response")
	}

	if testutil.WalletBalance(t, pool, testutil.WalletAlice) != testutil.AliceSeedBalance-100 {
		t.Fatalf("sender balance wrong")
	}
	if testutil.WalletBalance(t, pool, testutil.WalletBob) != testutil.BobSeedBalance+100 {
		t.Fatalf("receiver balance wrong")
	}
	if testutil.TransferCount(t, pool) != 1 {
		t.Fatalf("expected one transfer row")
	}
	if testutil.TransferStatus(t, pool, resp.TransferID) != domain.PROCESSED {
		t.Fatalf("expected PROCESSED in db")
	}
}

func TestCreateTransferLedger(t *testing.T) {
	svc, pool := newTestService(t)

	key := testutil.NewIdempotencyKey(t)
	req := domain.TransferRequest{
		IdempotencyKey: key,
		FromWalletID:   testutil.WalletAlice,
		ToWalletID:     testutil.WalletBob,
		Amount:         250,
	}

	resp, _, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if testutil.LedgerEntryCount(t, pool, resp.TransferID) != 2 {
		t.Fatalf("expected 2 ledger entries")
	}

	debitWallet, creditWallet, amount := testutil.LedgerEntryTypes(t, pool, resp.TransferID)
	if debitWallet != testutil.WalletAlice || creditWallet != testutil.WalletBob || amount != 250 {
		t.Fatalf("ledger entries mismatch: debit=%s credit=%s amount=%d", debitWallet, creditWallet, amount)
	}

	// retry should not add ledger rows
	_, _, err = svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("retry error: %v", err)
	}
	if testutil.TotalLedgerEntries(t, pool) != 2 {
		t.Fatalf("expected still 2 ledger entries after retry")
	}
}

func TestCreateTransferIdempotency(t *testing.T) {
	svc, pool := newTestService(t)

	key := testutil.NewIdempotencyKey(t)
	req := domain.TransferRequest{
		IdempotencyKey: key,
		FromWalletID:   testutil.WalletAlice,
		ToWalletID:     testutil.WalletBob,
		Amount:         100,
	}

	first, status1, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if status1 != http.StatusCreated {
		t.Fatalf("first call status: %d", status1)
	}

	second, status2, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if status2 != http.StatusCreated {
		t.Fatalf("second call status: %d", status2)
	}
	if first.TransferID != second.TransferID {
		t.Fatalf("transfer ids differ: %s vs %s", first.TransferID, second.TransferID)
	}
	if !testutil.HasIdempotencyRecord(t, pool, key) {
		t.Fatal("expected idempotency record")
	}
	if testutil.WalletBalance(t, pool, testutil.WalletAlice) != testutil.AliceSeedBalance-100 {
		t.Fatal("sender balance changed twice")
	}
}

func TestCreateTransferIdempotencyOnFailure(t *testing.T) {
	svc, pool := newTestService(t)

	testutil.SetWalletBalance(t, pool, testutil.WalletAlice, 50)

	key := testutil.NewIdempotencyKey(t)
	req := domain.TransferRequest{
		IdempotencyKey: key,
		FromWalletID:   testutil.WalletAlice,
		ToWalletID:     testutil.WalletBob,
		Amount:         100,
	}

	first, status1, err := svc.CreateTransfer(context.Background(), req)
	if !errors.Is(err, domain.ErrInsufficientFunds) {
		t.Fatalf("expected insufficient funds, got %v", err)
	}
	if status1 != http.StatusConflict {
		t.Fatalf("expected 409, got %d", status1)
	}

	second, status2, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("retry should return cached failed response without error, got %v", err)
	}
	if status2 != http.StatusConflict {
		t.Fatalf("retry expected 409, got %d", status2)
	}
	if second.Status != domain.FAILED {
		t.Fatalf("retry expected FAILED status, got %s", second.Status)
	}
	if first.TransferID != second.TransferID {
		t.Fatal("transfer ids should match on retry")
	}
	if testutil.WalletBalance(t, pool, testutil.WalletAlice) != 50 {
		t.Fatal("balance should not change on failed transfer")
	}
	if testutil.LedgerEntryCount(t, pool, first.TransferID) != 0 {
		t.Fatal("failed transfer should have no ledger entries")
	}
}

func TestCreateTransferInsufficientFunds(t *testing.T) {
	svc, pool := newTestService(t)

	testutil.SetWalletBalance(t, pool, testutil.WalletAlice, 50)

	key := testutil.NewIdempotencyKey(t)
	req := domain.TransferRequest{
		IdempotencyKey: key,
		FromWalletID:   testutil.WalletAlice,
		ToWalletID:     testutil.WalletBob,
		Amount:         100,
	}

	resp, status, err := svc.CreateTransfer(context.Background(), req)
	if !errors.Is(err, domain.ErrInsufficientFunds) {
		t.Fatalf("expected insufficient funds, got %v", err)
	}
	if status != http.StatusConflict {
		t.Fatalf("expected 409, got %d", status)
	}
	if resp.Status != domain.FAILED {
		t.Fatalf("expected FAILED status, got %s", resp.Status)
	}
	if testutil.TransferStatus(t, pool, resp.TransferID) != domain.FAILED {
		t.Fatal("expected FAILED in db")
	}
	if testutil.WalletBalance(t, pool, testutil.WalletAlice) != 50 {
		t.Fatal("sender balance should not change")
	}
	if testutil.WalletBalance(t, pool, testutil.WalletBob) != testutil.BobSeedBalance {
		t.Fatal("receiver balance should not change")
	}
}

func TestCreateTransferWalletNotFound(t *testing.T) {
	svc, _ := newTestService(t)

	key := testutil.NewIdempotencyKey(t)
	req := domain.TransferRequest{
		IdempotencyKey: key,
		FromWalletID:   "missing-wallet",
		ToWalletID:     testutil.WalletBob,
		Amount:         100,
	}

	_, status, err := svc.CreateTransfer(context.Background(), req)
	if !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("expected wallet not found, got %v", err)
	}
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", status)
	}
}

func TestCreateTransferConcurrentDebits(t *testing.T) {
	svc, pool := newTestService(t)

	testutil.SetWalletBalance(t, pool, testutil.WalletAlice, 150)

	type result struct {
		status int
		err    error
	}

	results := make([]result, 2)
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, status, err := svc.CreateTransfer(context.Background(), domain.TransferRequest{
				IdempotencyKey: testutil.NewIdempotencyKey(t),
				FromWalletID:   testutil.WalletAlice,
				ToWalletID:     testutil.WalletBob,
				Amount:         100,
			})
			results[idx] = result{status, err}
		}(i)
	}
	wg.Wait()

	successes := 0
	failures := 0
	for _, r := range results {
		switch {
		case r.err == nil && r.status == http.StatusCreated:
			successes++
		case errors.Is(r.err, domain.ErrInsufficientFunds):
			failures++
		default:
			t.Fatalf("unexpected result: status=%d err=%v", r.status, r.err)
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("expected 1 success and 1 failure, got %d success %d failure", successes, failures)
	}
	if balance := testutil.WalletBalance(t, pool, testutil.WalletAlice); balance != 50 {
		t.Fatalf("expected balance 50, got %d", balance)
	}
}

func TestCreateTransferConcurrentMany(t *testing.T) {
	svc, pool := newTestService(t)

	testutil.SetWalletBalance(t, pool, testutil.WalletAlice, 1000)

	const n = 10
	var wg sync.WaitGroup
	successes := make(chan bool, n)

	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, status, err := svc.CreateTransfer(context.Background(), domain.TransferRequest{
				IdempotencyKey: testutil.NewIdempotencyKey(t),
				FromWalletID:   testutil.WalletAlice,
				ToWalletID:     testutil.WalletBob,
				Amount:         100,
			})
			successes <- err == nil && status == http.StatusCreated
		}()
	}
	wg.Wait()
	close(successes)

	okCount := 0
	for ok := range successes {
		if ok {
			okCount++
		}
	}
	if okCount != 10 {
		t.Fatalf("expected 10 successes, got %d", okCount)
	}
	if testutil.WalletBalance(t, pool, testutil.WalletAlice) != 0 {
		t.Fatalf("expected balance 0, got %d", testutil.WalletBalance(t, pool, testutil.WalletAlice))
	}
}

func TestCreateTransferConcurrentBidirectional(t *testing.T) {
	svc, pool := newTestService(t)

	var wg sync.WaitGroup
	errs := make(chan error, 2)

	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _, err := svc.CreateTransfer(context.Background(), domain.TransferRequest{
			IdempotencyKey: testutil.NewIdempotencyKey(t),
			FromWalletID:   testutil.WalletAlice,
			ToWalletID:     testutil.WalletBob,
			Amount:         100,
		})
		errs <- err
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _, err := svc.CreateTransfer(context.Background(), domain.TransferRequest{
			IdempotencyKey: testutil.NewIdempotencyKey(t),
			FromWalletID:   testutil.WalletBob,
			ToWalletID:     testutil.WalletAlice,
			Amount:         50,
		})
		errs <- err
	}()

	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if testutil.WalletBalance(t, pool, testutil.WalletAlice) != testutil.AliceSeedBalance-100+50 {
		t.Fatalf("alice balance wrong: %d", testutil.WalletBalance(t, pool, testutil.WalletAlice))
	}
	if testutil.WalletBalance(t, pool, testutil.WalletBob) != testutil.BobSeedBalance+100-50 {
		t.Fatalf("bob balance wrong: %d", testutil.WalletBalance(t, pool, testutil.WalletBob))
	}
}

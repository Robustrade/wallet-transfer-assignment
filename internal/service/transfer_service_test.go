package service_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/database"
	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
	"github.com/Robustrade/wallet-transfer-assignment/internal/testutil"
)

func TestMain(m *testing.M) {
	os.Exit(testutil.RunWithDatabase(m))
}

type outcome struct {
	transfer *domain.Transfer
	replayed bool
	err      error
}

func createWallet(t *testing.T, id string, balance int64) {
	t.Helper()
	svc := service.WalletService{}
	if _, err := svc.CreateWallet(context.Background(), id, balance); err != nil {
		t.Fatalf("create wallet %s: %v", id, err)
	}
}

func balanceOf(t *testing.T, walletID string) int64 {
	t.Helper()
	var balance int64
	if err := database.DB.Raw("SELECT balance FROM wallets WHERE id = ?", walletID).Scan(&balance).Error; err != nil {
		t.Fatalf("query balance: %v", err)
	}
	return balance
}

func countRows(t *testing.T, table string) int64 {
	t.Helper()
	var count int64
	if err := database.DB.Raw(fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count).Error; err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

func ledgerTotal(t *testing.T, entryType string) int64 {
	t.Helper()
	var total int64
	err := database.DB.Raw("SELECT COALESCE(SUM(amount), 0) FROM ledger_entries WHERE entry_type = ?", entryType).Scan(&total).Error
	if err != nil {
		t.Fatalf("sum %s entries: %v", entryType, err)
	}
	return total
}

func runConcurrently(t *testing.T, requests []domain.TransferRequest) []outcome {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	svc := service.TransferService{}
	outcomes := make([]outcome, len(requests))
	start := make(chan struct{})
	var wg sync.WaitGroup

	for i, req := range requests {
		wg.Add(1)
		go func(i int, req domain.TransferRequest) {
			defer wg.Done()
			<-start
			transfer, replayed, err := svc.CreateTransfer(ctx, req)
			outcomes[i] = outcome{transfer: transfer, replayed: replayed, err: err}
		}(i, req)
	}

	close(start)
	wg.Wait()

	for i, o := range outcomes {
		if o.err != nil {
			t.Fatalf("request %d failed with error: %v", i, o.err)
		}
	}
	return outcomes
}

func TestConcurrentDebitsNeverOverspend(t *testing.T) {
	testutil.ResetDatabase(t)
	createWallet(t, "wallet_1", 1000)
	createWallet(t, "wallet_2", 0)

	requests := make([]domain.TransferRequest, 20)
	for i := range requests {
		requests[i] = domain.TransferRequest{
			IdempotencyKey: fmt.Sprintf("debit-%d", i),
			FromWalletID:   "wallet_1",
			ToWalletID:     "wallet_2",
			Amount:         100,
		}
	}

	outcomes := runConcurrently(t, requests)

	var processed, failed int
	for _, o := range outcomes {
		switch o.transfer.Status {
		case domain.StatusProcessed:
			processed++
		case domain.StatusFailed:
			failed++
		}
	}
	if processed != 10 || failed != 10 {
		t.Errorf("processed=%d failed=%d, want exactly 10 and 10", processed, failed)
	}
	if got := balanceOf(t, "wallet_1"); got != 0 {
		t.Errorf("wallet_1 balance = %d, want 0", got)
	}
	if got := balanceOf(t, "wallet_2"); got != 1000 {
		t.Errorf("wallet_2 balance = %d, want 1000", got)
	}
	if n := countRows(t, "ledger_entries"); n != 20 {
		t.Errorf("ledger entries = %d, want 20 (10 processed transfers x 2)", n)
	}
	if debits, credits := ledgerTotal(t, "DEBIT"), ledgerTotal(t, "CREDIT"); debits != credits {
		t.Errorf("ledger unbalanced: debits=%d credits=%d", debits, credits)
	}
}

func TestConcurrentRetriesWithSameKeyExecuteOnce(t *testing.T) {
	testutil.ResetDatabase(t)
	createWallet(t, "wallet_1", 1000)
	createWallet(t, "wallet_2", 0)

	requests := make([]domain.TransferRequest, 20)
	for i := range requests {
		requests[i] = domain.TransferRequest{
			IdempotencyKey: "same-key",
			FromWalletID:   "wallet_1",
			ToWalletID:     "wallet_2",
			Amount:         100,
		}
	}

	outcomes := runConcurrently(t, requests)

	firstID := outcomes[0].transfer.ID
	executed := 0
	for i, o := range outcomes {
		if o.transfer.ID != firstID {
			t.Errorf("request %d got transfer %s, want every request to get %s", i, o.transfer.ID, firstID)
		}
		if !o.replayed {
			executed++
		}
	}
	if executed != 1 {
		t.Errorf("%d requests executed the transfer, want exactly 1 (the rest replayed)", executed)
	}
	if n := countRows(t, "transfers"); n != 1 {
		t.Errorf("transfers = %d, want 1", n)
	}
	if got := balanceOf(t, "wallet_1"); got != 900 {
		t.Errorf("wallet_1 balance = %d, want 900 (debited once)", got)
	}
	if got := balanceOf(t, "wallet_2"); got != 100 {
		t.Errorf("wallet_2 balance = %d, want 100 (credited once)", got)
	}
	if n := countRows(t, "ledger_entries"); n != 2 {
		t.Errorf("ledger entries = %d, want 2", n)
	}
}

func TestConcurrentOppositeTransfersDoNotDeadlock(t *testing.T) {
	testutil.ResetDatabase(t)
	createWallet(t, "wallet_1", 1000)
	createWallet(t, "wallet_2", 1000)

	requests := make([]domain.TransferRequest, 20)
	for i := range requests {
		from, to := "wallet_1", "wallet_2"
		if i%2 == 1 {
			from, to = to, from
		}
		requests[i] = domain.TransferRequest{
			IdempotencyKey: fmt.Sprintf("swap-%d", i),
			FromWalletID:   from,
			ToWalletID:     to,
			Amount:         10,
		}
	}

	outcomes := runConcurrently(t, requests)

	for i, o := range outcomes {
		if o.transfer.Status != domain.StatusProcessed {
			t.Errorf("request %d status = %s, want PROCESSED", i, o.transfer.Status)
		}
	}
	if total := balanceOf(t, "wallet_1") + balanceOf(t, "wallet_2"); total != 2000 {
		t.Errorf("total balance = %d, want 2000 (money must be conserved)", total)
	}
	if got := balanceOf(t, "wallet_1"); got != 1000 {
		t.Errorf("wallet_1 balance = %d, want 1000 (10 transfers each way)", got)
	}
	if debits, credits := ledgerTotal(t, "DEBIT"), ledgerTotal(t, "CREDIT"); debits != credits {
		t.Errorf("ledger unbalanced: debits=%d credits=%d", debits, credits)
	}
}

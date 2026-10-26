package service_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"wallettransfer/internal/domain"
	"wallettransfer/internal/repository"
	"wallettransfer/internal/service"
)

// Each test holds a real PostgreSQL wallet row lock, starts requests, and
// observes them waiting inside PostgreSQL (pg_stat_activity) before releasing
// the lock. A goroutine start signal alone would not prove overlap.
// These tests require the same PostgreSQL role to inspect its own sessions.
func TestPostgresConcurrentTransfers(t *testing.T) {
	t.Run("competing debits never overspend", func(t *testing.T) {
		db := concurrencyDatabase(t)
		seedWallets(t, db, map[string]int64{"A": 100, "B": 0, "C": 0})
		svc := service.New(repository.NewPostgres(db))
		release := holdWalletLock(t, db, "A")

		requests := []domain.TransferRequest{
			{IdempotencyKey: "debit-to-B", FromWalletID: "A", ToWalletID: "B", Amount: 80},
			{IdempotencyKey: "debit-to-C", FromWalletID: "A", ToWalletID: "C", Amount: 80},
		}
		results := launchTransfers(t, svc, requests)
		waitForPostgresWaits(t, db, 2, 0)
		release()
		outcomes := collectTransfers(t, results, len(requests))

		processed, failed := 0, 0
		for i, result := range outcomes {
			if result.err != nil {
				t.Fatalf("request %d: %v", i, result.err)
			}
			switch result.transfer.Status {
			case domain.StatusProcessed:
				processed++
				assertStoredTransfer(t, db, result.transfer.ID, requests[i], domain.StatusProcessed, "")
				assertLedger(t, db, result.transfer.ID, []ledgerExpectation{
					{wallet: "A", kind: "DEBIT", amount: 80},
					{wallet: requests[i].ToWalletID, kind: "CREDIT", amount: 80},
				})
			case domain.StatusFailed:
				failed++
				if result.transfer.FailureReason != domain.ReasonInsufficientFunds {
					t.Errorf("unexpected failure: %+v", result.transfer)
				}
				assertStoredTransfer(t, db, result.transfer.ID, requests[i], domain.StatusFailed, domain.ReasonInsufficientFunds)
				assertLedger(t, db, result.transfer.ID, nil)
			default:
				t.Errorf("unexpected outcome: %+v", result.transfer)
			}
		}
		if processed != 1 || failed != 1 {
			t.Fatalf("processed=%d failed=%d; want one of each", processed, failed)
		}
		assertBalance(t, db, "A", 20)
		assertRecipientBalances(t, db, "B", "C", 80)
		assertTransferCount(t, db, 2)
		assertTotalLedgerCount(t, db, 2)
		assertReconciles(t, db)
	})

	t.Run("concurrent identical key returns one stored result", func(t *testing.T) {
		db := concurrencyDatabase(t)
		seedWallets(t, db, map[string]int64{"A": 1000, "B": 50})
		svc := service.New(repository.NewPostgres(db))
		release := holdWalletLock(t, db, "A")

		const n = 8
		req := domain.TransferRequest{IdempotencyKey: "duplicate-concurrent", FromWalletID: "A", ToWalletID: "B", Amount: 100}
		requests := make([]domain.TransferRequest, n)
		for i := range requests {
			requests[i] = req
		}
		results := launchTransfers(t, svc, requests)
		// The first claimant waits on A. Subsequent claimants wait on the
		// *uncommitted unique key*, not on wallet rows.
		waitForPostgresWaits(t, db, n, n-1)
		release()
		outcomes := collectTransfers(t, results, n)

		first := outcomes[0].transfer
		if first.ID == "" || first.Status != domain.StatusProcessed {
			t.Fatalf("first outcome: %+v; error=%v", first, outcomes[0].err)
		}
		for i, o := range outcomes {
			if o.err != nil || o.transfer != first {
				t.Errorf("request %d outcome=%+v err=%v; expected %+v", i, o.transfer, o.err, first)
			}
		}
		assertBalance(t, db, "A", 900)
		assertBalance(t, db, "B", 150)
		assertStoredTransfer(t, db, first.ID, req, domain.StatusProcessed, "")
		assertLedger(t, db, first.ID, []ledgerExpectation{
			{wallet: "A", kind: "DEBIT", amount: 100},
			{wallet: "B", kind: "CREDIT", amount: 100},
		})
		assertTransferKeyCount(t, db, req.IdempotencyKey, 1)
		assertTransferCount(t, db, 1)
		assertTotalLedgerCount(t, db, 2)
		assertReconciles(t, db)
	})

	t.Run("concurrent conflicting key beats nonexistent wallet", func(t *testing.T) {
		db := concurrencyDatabase(t)
		seedWallets(t, db, map[string]int64{"A": 500, "B": 100})
		svc := service.New(repository.NewPostgres(db))
		release := holdWalletLock(t, db, "A")

		original := domain.TransferRequest{IdempotencyKey: "conflict-concurrent", FromWalletID: "A", ToWalletID: "B", Amount: 100}
		changed := domain.TransferRequest{IdempotencyKey: "conflict-concurrent", FromWalletID: "A", ToWalletID: "wallet-does-not-exist", Amount: 100}
		// Stage the original claimant first so we know it owns the key but
		// has not yet committed, then race the conflicting request into it.
		first := launchTransfers(t, svc, []domain.TransferRequest{original})
		waitForPostgresWaits(t, db, 1, 0)
		second := launchTransfers(t, svc, []domain.TransferRequest{changed})
		waitForPostgresWaits(t, db, 2, 1)
		release()
		firstResult := collectTransfers(t, first, 1)[0]
		secondResult := collectTransfers(t, second, 1)[0]

		if firstResult.err != nil || firstResult.transfer.ID == "" || firstResult.transfer.Status != domain.StatusProcessed {
			t.Fatalf("original: result=%+v err=%v", firstResult.transfer, firstResult.err)
		}
		if !errors.Is(secondResult.err, domain.ErrIdempotencyConflict) || secondResult.transfer != (domain.TransferResult{}) {
			t.Fatalf("changed request: result=%+v err=%v; want idempotency conflict", secondResult.transfer, secondResult.err)
		}
		assertBalance(t, db, "A", 400)
		assertBalance(t, db, "B", 200)
		assertStoredTransfer(t, db, firstResult.transfer.ID, original, domain.StatusProcessed, "")
		assertLedger(t, db, firstResult.transfer.ID, []ledgerExpectation{
			{wallet: "A", kind: "DEBIT", amount: 100},
			{wallet: "B", kind: "CREDIT", amount: 100},
		})
		assertTransferKeyCount(t, db, original.IdempotencyKey, 1)
		assertTransferCount(t, db, 1)
		assertTotalLedgerCount(t, db, 2)
		assertReconciles(t, db)
	})

	t.Run("opposite directions finish without inverted wallet lock deadlock", func(t *testing.T) {
		db := concurrencyDatabase(t)
		seedWallets(t, db, map[string]int64{"A": 100, "B": 100})
		svc := service.New(repository.NewPostgres(db))
		release := holdWalletLock(t, db, "A")

		requests := []domain.TransferRequest{
			{IdempotencyKey: "opposite-A-B", FromWalletID: "A", ToWalletID: "B", Amount: 80},
			{IdempotencyKey: "opposite-B-A", FromWalletID: "B", ToWalletID: "A", Amount: 50},
		}
		results := launchTransfers(t, svc, requests)
		// Both requests wait on A, since it is the first lock by sorted ID.
		waitForPostgresWaits(t, db, 2, 0)
		release()
		outcomes := collectTransfers(t, results, 2)
		for i, o := range outcomes {
			if o.err != nil || o.transfer.ID == "" || o.transfer.Status != domain.StatusProcessed {
				t.Fatalf("request %d result=%+v err=%v", i, o.transfer, o.err)
			}
			assertStoredTransfer(t, db, o.transfer.ID, requests[i], domain.StatusProcessed, "")
			assertLedger(t, db, o.transfer.ID, []ledgerExpectation{
				{wallet: requests[i].FromWalletID, kind: "DEBIT", amount: requests[i].Amount},
				{wallet: requests[i].ToWalletID, kind: "CREDIT", amount: requests[i].Amount},
			})
		}
		assertBalance(t, db, "A", 70)
		assertBalance(t, db, "B", 130)
		assertTransferCount(t, db, 2)
		assertTotalLedgerCount(t, db, 4)
		assertReconciles(t, db)
	})

	t.Run("unrelated wallets complete while another transfer is blocked", func(t *testing.T) {
		db := concurrencyDatabase(t)
		seedWallets(t, db, map[string]int64{"A": 500, "B": 100, "C": 400, "D": 200})
		svc := service.New(repository.NewPostgres(db))
		release := holdWalletLock(t, db, "A")

		blockedReq := domain.TransferRequest{IdempotencyKey: "blocked-A-B", FromWalletID: "A", ToWalletID: "B", Amount: 100}
		freeReq := domain.TransferRequest{IdempotencyKey: "free-C-D", FromWalletID: "C", ToWalletID: "D", Amount: 150}
		blocked := launchTransfers(t, svc, []domain.TransferRequest{blockedReq})
		waitForPostgresWaits(t, db, 1, 0)
		free := launchTransfers(t, svc, []domain.TransferRequest{freeReq})
		// This MUST finish while the A lock remains held. Otherwise an
		// accidental global lock or pool bottleneck serializes unrelated work.
		freeOutcome := collectTransfers(t, free, 1)[0]
		if freeOutcome.err != nil || freeOutcome.transfer.Status != domain.StatusProcessed {
			t.Fatalf("unrelated operation failed/blocked: result=%+v err=%v", freeOutcome.transfer, freeOutcome.err)
		}
		assertBalance(t, db, "A", 500)
		assertBalance(t, db, "B", 100)
		assertBalance(t, db, "C", 250)
		assertBalance(t, db, "D", 350)
		release()
		blockedOutcome := collectTransfers(t, blocked, 1)[0]
		if blockedOutcome.err != nil || blockedOutcome.transfer.Status != domain.StatusProcessed {
			t.Fatalf("blocked operation: result=%+v err=%v", blockedOutcome.transfer, blockedOutcome.err)
		}
		assertBalance(t, db, "A", 400)
		assertBalance(t, db, "B", 200)
		assertBalance(t, db, "C", 250)
		assertBalance(t, db, "D", 350)
		for _, row := range []struct {
			r   domain.TransferResult
			req domain.TransferRequest
		}{{blockedOutcome.transfer, blockedReq}, {freeOutcome.transfer, freeReq}} {
			assertStoredTransfer(t, db, row.r.ID, row.req, domain.StatusProcessed, "")
			assertLedger(t, db, row.r.ID, []ledgerExpectation{
				{wallet: row.req.FromWalletID, kind: "DEBIT", amount: row.req.Amount},
				{wallet: row.req.ToWalletID, kind: "CREDIT", amount: row.req.Amount},
			})
		}
		assertTransferCount(t, db, 2)
		assertTotalLedgerCount(t, db, 4)
		assertReconciles(t, db)
	})
}

func concurrencyDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db := testDatabase(t)
	// Test helpers use a bounded pool in earlier increments (4 connections).
	// Concurrent requests + a lock holder + a pg_stat_activity observer require
	// more so pool saturation cannot masquerade as PostgreSQL row contention.
	db.SetMaxOpenConns(24)
	return db
}

func holdWalletLock(t *testing.T, db *sql.DB, id string) func() {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	var got string
	if err := tx.QueryRowContext(ctx, "SELECT id FROM wallets WHERE id=$1 FOR NO KEY UPDATE", id).Scan(&got); err != nil {
		_ = tx.Rollback()
		cancel()
		t.Fatal(err)
	}
	var once sync.Once
	release := func() { once.Do(func() { _ = tx.Rollback(); cancel() }) }
	t.Cleanup(release)
	return release
}

type transferOutcome struct {
	transfer domain.TransferResult
	err      error
}

// Each channel index maps to the request at the same index. Context deadlines
// prevent a broken lock or deadlock test from hanging indefinitely.
func launchTransfers(t *testing.T, svc *service.Service, requests []domain.TransferRequest) []<-chan transferOutcome {
	t.Helper()
	ready := make(chan struct{})
	outputs := make([]<-chan transferOutcome, len(requests))
	for i, req := range requests {
		ch := make(chan transferOutcome, 1)
		outputs[i] = ch
		go func(req domain.TransferRequest) {
			<-ready
			ctx, cancel := context.WithTimeout(context.Background(), 18*time.Second)
			defer cancel()
			result, err := svc.Transfer(ctx, req)
			ch <- transferOutcome{transfer: result, err: err}
		}(req)
	}
	close(ready)
	return outputs
}

func collectTransfers(t *testing.T, outputs []<-chan transferOutcome, count int) []transferOutcome {
	t.Helper()
	results := make([]transferOutcome, count)
	for i := 0; i < count; i++ {
		select {
		case result := <-outputs[i]:
			results[i] = result
		case <-time.After(20 * time.Second):
			t.Fatalf("transfer %d did not finish in 20 seconds (possible deadlock or connection starvation)", i)
		}
	}
	return results
}

// PostgreSQL reports actual lock waits. The second argument is how many of
// those waits must be blocked at INSERT INTO transfers (unique-key arbitration).
func waitForPostgresWaits(t *testing.T, db *sql.DB, minimum, minimumKeyClaim int) {
	t.Helper()
	var appName string
	if err := db.QueryRow("SELECT current_setting('application_name')").Scan(&appName); err != nil {
		t.Fatalf("application_name: %v", err)
	}
	if !strings.HasPrefix(appName, "wallet_concurrency_") {
		t.Fatalf("unsafe/unscoped application_name: %q", appName)
	}
	const query = `SELECT count(*) FILTER (WHERE wait_event_type='Lock'),
        count(*) FILTER (WHERE wait_event_type='Lock' AND query LIKE '%INSERT INTO transfers%')
        FROM pg_stat_activity WHERE application_name=$1 AND state='active'`
	deadline := time.Now().Add(9 * time.Second)
	var waits, keyWaits int
	for time.Now().Before(deadline) {
		if err := db.QueryRow(query, appName).Scan(&waits, &keyWaits); err != nil {
			t.Fatalf("inspect PostgreSQL lock waits: %v", err)
		}
		if waits >= minimum && keyWaits >= minimumKeyClaim {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("requests did not reach PostgreSQL overlapping lock waits within 9s: got %d waits (%d at unique-key claim), wanted >=%d (>= %d at claim); not a verified concurrency test", waits, keyWaits, minimum, minimumKeyClaim)
}

// This helper doesn't assume which request acquires the source lock first.
func assertRecipientBalances(t *testing.T, db *sql.DB, b, c string, transferred int64) {
	t.Helper()
	var bBalance, cBalance int64
	if err := db.QueryRow("SELECT balance FROM wallets WHERE id=$1", b).Scan(&bBalance); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT balance FROM wallets WHERE id=$1", c).Scan(&cBalance); err != nil {
		t.Fatal(err)
	}
	if !((bBalance == transferred && cBalance == 0) || (bBalance == 0 && cBalance == transferred)) {
		t.Errorf("unexpected receiving wallet balances: %s=%d %s=%d, want one %d and other zero", b, bBalance, c, cBalance, transferred)
	}
}

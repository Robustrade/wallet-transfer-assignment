package service_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Two transfers debit the same wallet at once. Row locks serialize them:
// correct balances, no double-spend, ledger matches balances.
func TestCreateTransfer_TwoConcurrentDebitsSameWallet(t *testing.T) {
	svc, pool := testEnv(t)
	ctx := context.Background()

	// Balance 50; each wants 40 → only one can succeed.
	if _, err := svc.CreateWallet(ctx, "shared", 50); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateWallet(ctx, "recv_a", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateWallet(ctx, "recv_b", 0); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	outcomes := make([]struct {
		code int
		err  error
	}, 2)

	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		out, err := svc.CreateTransfer(ctx, "t1", "shared", "recv_a", 40)
		outcomes[0].err = err
		if err == nil {
			outcomes[0].code = out.StatusCode
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		out, err := svc.CreateTransfer(ctx, "t2", "shared", "recv_b", 40)
		outcomes[1].err = err
		if err == nil {
			outcomes[1].code = out.StatusCode
		}
	}()
	close(start)
	wg.Wait()

	for i, o := range outcomes {
		if o.err != nil {
			t.Fatalf("transfer %d: %v", i, o.err)
		}
	}

	var processed, failed int
	for _, o := range outcomes {
		switch o.code {
		case 201:
			processed++
		case 422:
			failed++
		default:
			t.Fatalf("unexpected status %d", o.code)
		}
	}
	if processed != 1 || failed != 1 {
		t.Fatalf("processed=%d failed=%d, want 1 success and 1 insufficient_funds", processed, failed)
	}

	shared, _ := svc.GetWallet(ctx, "shared")
	a, _ := svc.GetWallet(ctx, "recv_a")
	b, _ := svc.GetWallet(ctx, "recv_b")
	if shared.Balance != 10 {
		t.Fatalf("shared balance=%d, want 10 (50-40)", shared.Balance)
	}
	if !((a.Balance == 40 && b.Balance == 0) || (a.Balance == 0 && b.Balance == 40)) {
		t.Fatalf("expected exactly one destination credited 40, got recv_a=%d recv_b=%d", a.Balance, b.Balance)
	}

	ledger := repository.NewLedgerRepository(pool)
	for _, id := range []string{"shared", "recv_a", "recv_b"} {
		w, _ := svc.GetWallet(ctx, id)
		net, err := ledger.SumSigned(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if w.Balance != w.OpeningBalance+net {
			t.Fatalf("%s: balance %d != opening %d + ledger net %d", id, w.Balance, w.OpeningBalance, net)
		}
	}

	var debitRows, creditRows, processedTransfers, failedTransfers int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM ledger_entries WHERE entry_type = 'DEBIT'`).Scan(&debitRows)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM ledger_entries WHERE entry_type = 'CREDIT'`).Scan(&creditRows)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM transfers WHERE status = 'PROCESSED'`).Scan(&processedTransfers)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM transfers WHERE status = 'FAILED'`).Scan(&failedTransfers)
	if debitRows != 1 || creditRows != 1 {
		t.Fatalf("ledger debit=%d credit=%d, want 1 each (failed transfer writes none)", debitRows, creditRows)
	}
	if processedTransfers != 1 || failedTransfers != 1 {
		t.Fatalf("transfers processed=%d failed=%d", processedTransfers, failedTransfers)
	}
}

// Ten concurrent transfers of 20 against balance 100: exactly five succeed,
// balance reaches 0, ledger debits sum to 100 — no double-spend.
func TestCreateTransfer_ConcurrentNoDoubleSpend(t *testing.T) {
	svc, pool := testEnv(t)
	ctx := context.Background()
	if _, err := svc.CreateWallet(ctx, "src", 100); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateWallet(ctx, "dst", 0); err != nil {
		t.Fatal(err)
	}

	const n = 10
	var processed, failed atomic.Int32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			key := fmt.Sprintf("race-%d", i)
			out, err := svc.CreateTransfer(ctx, key, "src", "dst", 20)
			if err != nil {
				t.Errorf("transfer %d: %v", i, err)
				return
			}
			switch out.StatusCode {
			case 201:
				processed.Add(1)
			case 422:
				failed.Add(1)
			default:
				t.Errorf("unexpected status %d", out.StatusCode)
			}
		}()
	}
	wg.Wait()

	if processed.Load() != 5 || failed.Load() != 5 {
		t.Fatalf("processed=%d failed=%d, want 5/5", processed.Load(), failed.Load())
	}
	src, _ := svc.GetWallet(ctx, "src")
	dst, _ := svc.GetWallet(ctx, "dst")
	if src.Balance != 0 || dst.Balance != 100 {
		t.Fatalf("balances src=%d dst=%d", src.Balance, dst.Balance)
	}

	ledger := repository.NewLedgerRepository(pool)
	sumSrc, err := ledger.SumSigned(ctx, "src")
	if err != nil {
		t.Fatal(err)
	}
	sumDst, err := ledger.SumSigned(ctx, "dst")
	if err != nil {
		t.Fatal(err)
	}
	if src.OpeningBalance+sumSrc != src.Balance || dst.OpeningBalance+sumDst != dst.Balance {
		t.Fatalf("ledger inconsistent: src net=%d dst net=%d", sumSrc, sumDst)
	}
	if sumSrc != -100 || sumDst != 100 {
		t.Fatalf("ledger nets src=%d dst=%d", sumSrc, sumDst)
	}
}

// Ten goroutines with the same idempotency key: exactly one transfer and one ledger pair.
func TestCreateTransfer_ConcurrentSameKeyExactlyOnce(t *testing.T) {
	svc, pool := testEnv(t)
	ctx := context.Background()
	_, _ = svc.CreateWallet(ctx, "src", 100)
	_, _ = svc.CreateWallet(ctx, "dst", 0)

	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)
	results := make([]string, n)
	codes := make([]int, n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			out, err := svc.CreateTransfer(ctx, "same-key", "src", "dst", 40)
			if err != nil {
				t.Errorf("%v", err)
				return
			}
			codes[i] = out.StatusCode
			if out.Transfer != nil {
				results[i] = out.Transfer.TransferID
			} else if out.IsReplay() {
				// extract transferId from replay body via a second get is unnecessary;
				// all must share status 201 and leave a single transfer.
				results[i] = "replay"
			}
		}()
	}
	wg.Wait()

	for i, code := range codes {
		if code != 201 {
			t.Fatalf("goroutine %d status=%d", i, code)
		}
	}
	src, _ := svc.GetWallet(ctx, "src")
	if src.Balance != 60 {
		t.Fatalf("balance=%d, want 60 (one debit of 40)", src.Balance)
	}

	var transferCount int
	err := pool.QueryRow(ctx, `SELECT count(*) FROM transfers WHERE idempotency_key = @idempotency_key`,
		pgx.NamedArgs{"idempotency_key": "same-key"}).Scan(&transferCount)
	if err != nil {
		t.Fatal(err)
	}
	if transferCount != 1 {
		t.Fatalf("transfers=%d, want 1", transferCount)
	}

	var ledgerCount int
	err = pool.QueryRow(ctx, `
		SELECT count(*) FROM ledger_entries le
		JOIN transfers t ON t.id = le.transfer_id
		WHERE t.idempotency_key = @idempotency_key`,
		pgx.NamedArgs{"idempotency_key": "same-key"}).Scan(&ledgerCount)
	if err != nil {
		t.Fatal(err)
	}
	if ledgerCount != 2 {
		t.Fatalf("ledger rows=%d, want 2", ledgerCount)
	}

	// All fresh outcomes share one transferId; replays are fine too.
	var firstID string
	for _, id := range results {
		if id == "" || id == "replay" {
			continue
		}
		if firstID == "" {
			firstID = id
			continue
		}
		if id != firstID {
			t.Fatalf("multiple transfer ids: %s vs %s", firstID, id)
		}
	}
	if firstID != "" {
		if _, err := uuid.Parse(firstID); err != nil {
			t.Fatal(err)
		}
	}
}

// Concurrent A→B and B→A must finish without deadlock; balances stay non-negative and reconcile.
func TestCreateTransfer_ConcurrentOppositeNoDeadlock(t *testing.T) {
	svc, pool := testEnv(t)
	ctx := context.Background()
	_, _ = svc.CreateWallet(ctx, "A", 50)
	_, _ = svc.CreateWallet(ctx, "B", 50)

	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := svc.CreateTransfer(ctx, "ab", "A", "B", 30)
			if err != nil {
				t.Errorf("A→B: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			_, err := svc.CreateTransfer(ctx, "ba", "B", "A", 30)
			if err != nil {
				t.Errorf("B→A: %v", err)
			}
		}()
		wg.Wait()
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("deadlock: opposite-direction transfers did not finish")
	}

	a, _ := svc.GetWallet(ctx, "A")
	b, _ := svc.GetWallet(ctx, "B")
	if a.Balance < 0 || b.Balance < 0 {
		t.Fatalf("negative balance A=%d B=%d", a.Balance, b.Balance)
	}
	if a.Balance+b.Balance != 100 {
		t.Fatalf("conservation A=%d B=%d", a.Balance, b.Balance)
	}

	ledger := repository.NewLedgerRepository(pool)
	for _, id := range []string{"A", "B"} {
		w, _ := svc.GetWallet(ctx, id)
		net, err := ledger.SumSigned(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if w.Balance != w.OpeningBalance+net {
			t.Fatalf("%s: balance %d != opening %d + net %d", id, w.Balance, w.OpeningBalance, net)
		}
	}
}

// Disjoint wallet pairs take different row locks, so concurrent transfers proceed
// without serializing on each other. All succeed; balances and ledger stay consistent.
func TestCreateTransfer_ConcurrentDisjointPairs(t *testing.T) {
	svc, pool := testEnv(t)
	ctx := context.Background()

	const pairs = 8
	for i := 0; i < pairs; i++ {
		src := fmt.Sprintf("src_%d", i)
		dst := fmt.Sprintf("dst_%d", i)
		if _, err := svc.CreateWallet(ctx, src, 100); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.CreateWallet(ctx, dst, 0); err != nil {
			t.Fatal(err)
		}
	}

	start := make(chan struct{})
	done := make(chan struct{})
	var wg sync.WaitGroup
	var okCount atomic.Int32
	wg.Add(pairs)
	go func() {
		defer close(done)
		for i := 0; i < pairs; i++ {
			i := i
			go func() {
				defer wg.Done()
				<-start
				out, err := svc.CreateTransfer(ctx,
					fmt.Sprintf("disjoint-%d", i),
					fmt.Sprintf("src_%d", i),
					fmt.Sprintf("dst_%d", i),
					25,
				)
				if err != nil {
					t.Errorf("pair %d: %v", i, err)
					return
				}
				if out.StatusCode != 201 {
					t.Errorf("pair %d status=%d, want 201", i, out.StatusCode)
					return
				}
				okCount.Add(1)
			}()
		}
		wg.Wait()
	}()
	close(start)

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("deadlock or hang: disjoint transfers did not finish")
	}

	if okCount.Load() != pairs {
		t.Fatalf("processed=%d, want %d", okCount.Load(), pairs)
	}

	ledger := repository.NewLedgerRepository(pool)
	for i := 0; i < pairs; i++ {
		srcID := fmt.Sprintf("src_%d", i)
		dstID := fmt.Sprintf("dst_%d", i)
		src, err := svc.GetWallet(ctx, srcID)
		if err != nil {
			t.Fatal(err)
		}
		dst, err := svc.GetWallet(ctx, dstID)
		if err != nil {
			t.Fatal(err)
		}
		if src.Balance != 75 || dst.Balance != 25 {
			t.Fatalf("pair %d balances src=%d dst=%d, want 75/25", i, src.Balance, dst.Balance)
		}
		for _, id := range []string{srcID, dstID} {
			w, _ := svc.GetWallet(ctx, id)
			net, err := ledger.SumSigned(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if w.Balance != w.OpeningBalance+net {
				t.Fatalf("%s: balance %d != opening %d + net %d", id, w.Balance, w.OpeningBalance, net)
			}
		}
	}
}

// Concurrent GetWallet during contended debits must not hang or return negative balances.
func TestGetWallet_ConcurrentReadsDuringDebits(t *testing.T) {
	svc, _ := testEnv(t)
	ctx := context.Background()
	_, _ = svc.CreateWallet(ctx, "src", 100)
	_, _ = svc.CreateWallet(ctx, "dst", 0)

	start := make(chan struct{})
	var wg sync.WaitGroup
	var readErrors atomic.Int32

	for i := 0; i < 10; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := svc.CreateTransfer(ctx, fmt.Sprintf("w-%d", i), "src", "dst", 20)
			if err != nil {
				t.Errorf("transfer: %v", err)
			}
		}()
	}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			w, err := svc.GetWallet(ctx, "src")
			if err != nil {
				readErrors.Add(1)
				t.Errorf("GetWallet: %v", err)
				return
			}
			if w.Balance < 0 {
				readErrors.Add(1)
				t.Errorf("negative balance %d", w.Balance)
			}
		}()
	}
	close(start)
	wg.Wait()
	if readErrors.Load() != 0 {
		t.Fatalf("read errors=%d", readErrors.Load())
	}
}

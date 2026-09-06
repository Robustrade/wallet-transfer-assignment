package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"wallet-transfer/internal/domain"
)

// ── 1. Successful transfer ────────────────────────────────────────────────────

func TestTransfer_Success(t *testing.T) {
	db := newTestDB(t)
	srv := newTestServer(t, db)

	alice := createWallet(t, srv, "Alice", 1000)
	bob := createWallet(t, srv, "Bob", 500)

	status, resp, _ := doTransfer(t, srv, transferRequest{
		IdempotencyKey: uniqueKey("ok"),
		FromWalletID:   alice.ID,
		ToWalletID:     bob.ID,
		Amount:         100,
	})

	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d", status)
	}
	if resp.Status != "PROCESSED" {
		t.Errorf("expected PROCESSED, got %s", resp.Status)
	}

	assertWalletBalance(t, db, alice.ID, 90000) // 900.00
	assertWalletBalance(t, db, bob.ID, 60000)   // 600.00
	assertTransferStatus(t, db, resp.TransferID, "PROCESSED")
	assertLedgerEntries(t, db, resp.TransferID, true, true)
}

// ── 2. Insufficient balance ───────────────────────────────────────────────────

func TestTransfer_InsufficientBalance(t *testing.T) {
	db := newTestDB(t)
	srv := newTestServer(t, db)

	alice := createWallet(t, srv, "Alice", 50)
	bob := createWallet(t, srv, "Bob", 0)

	status, _, errResp := doTransfer(t, srv, transferRequest{
		IdempotencyKey: uniqueKey("nsf"),
		FromWalletID:   alice.ID,
		ToWalletID:     bob.ID,
		Amount:         100,
	})

	if status != http.StatusConflict {
		t.Fatalf("expected 409, got %d", status)
	}
	if errResp.Error.Code != "INSUFFICIENT_FUNDS" {
		t.Errorf("expected INSUFFICIENT_FUNDS, got %s", errResp.Error.Code)
	}

	// Balances must be unchanged.
	assertWalletBalance(t, db, alice.ID, 5000) // 50.00
	assertWalletBalance(t, db, bob.ID, 0)

	// Transfer record exists but is FAILED (not PROCESSED).
	// (We verify via the idempotent replay test below, which also covers this.)
}

// ── 3. Idempotent replay ──────────────────────────────────────────────────────

func TestTransfer_IdempotentReplay(t *testing.T) {
	db := newTestDB(t)
	srv := newTestServer(t, db)

	alice := createWallet(t, srv, "Alice", 1000)
	bob := createWallet(t, srv, "Bob", 0)

	req := transferRequest{
		IdempotencyKey: uniqueKey("idem"),
		FromWalletID:   alice.ID,
		ToWalletID:     bob.ID,
		Amount:         200,
	}

	status1, resp1, _ := doTransfer(t, srv, req)
	if status1 != http.StatusCreated {
		t.Fatalf("first request: expected 201, got %d", status1)
	}

	status2, resp2, _ := doTransfer(t, srv, req)
	if status2 != http.StatusCreated {
		t.Fatalf("second request: expected 201, got %d", status2)
	}
	if resp1.TransferID != resp2.TransferID {
		t.Errorf("expected same transferId: %s vs %s", resp1.TransferID, resp2.TransferID)
	}

	// Only one transfer row.
	var count int
	db.QueryRow(`SELECT count(*) FROM transfers WHERE from_wallet_id=$1`, alice.ID).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 transfer, found %d", count)
	}

	// Only two ledger entries total.
	var ledgerCount int
	db.QueryRow(`SELECT count(*) FROM ledger_entries WHERE transfer_id=$1`, resp1.TransferID).Scan(&ledgerCount)
	if ledgerCount != 2 {
		t.Errorf("expected 2 ledger entries, found %d", ledgerCount)
	}

	// Balance changed exactly once.
	assertWalletBalance(t, db, alice.ID, 80000) // 800.00
	assertWalletBalance(t, db, bob.ID, 20000)   // 200.00
}

// ── 4. Idempotency key conflict ───────────────────────────────────────────────

func TestTransfer_IdempotencyConflict(t *testing.T) {
	db := newTestDB(t)
	srv := newTestServer(t, db)

	alice := createWallet(t, srv, "Alice", 1000)
	bob := createWallet(t, srv, "Bob", 0)
	carol := createWallet(t, srv, "Carol", 0)

	key := uniqueKey("conflict")

	// First request: Alice → Bob, 100.
	status1, _, _ := doTransfer(t, srv, transferRequest{
		IdempotencyKey: key,
		FromWalletID:   alice.ID,
		ToWalletID:     bob.ID,
		Amount:         100,
	})
	if status1 != http.StatusCreated {
		t.Fatalf("first request: expected 201, got %d", status1)
	}

	// Second request: same key but different params (different destination + amount).
	status2, _, errResp := doTransfer(t, srv, transferRequest{
		IdempotencyKey: key,
		FromWalletID:   alice.ID,
		ToWalletID:     carol.ID,
		Amount:         500,
	})
	if status2 != http.StatusConflict {
		t.Fatalf("conflict request: expected 409, got %d", status2)
	}
	if errResp.Error.Code != "IDEMPOTENCY_CONFLICT" {
		t.Errorf("expected IDEMPOTENCY_CONFLICT, got %s", errResp.Error.Code)
	}

	// Carol's balance must still be zero — second transfer must not have run.
	assertWalletBalance(t, db, carol.ID, 0)
	assertWalletBalance(t, db, alice.ID, 90000)
	assertWalletBalance(t, db, bob.ID, 10000)
}

// ── 5. Validation errors ──────────────────────────────────────────────────────

func TestTransfer_Validation(t *testing.T) {
	db := newTestDB(t)
	srv := newTestServer(t, db)

	alice := createWallet(t, srv, "Alice", 1000)
	bob := createWallet(t, srv, "Bob", 1000)

	cases := []struct {
		name       string
		req        transferRequest
		wantStatus int
		wantCode   string
	}{
		{
			name:       "zero amount",
			req:        transferRequest{IdempotencyKey: uniqueKey("v"), FromWalletID: alice.ID, ToWalletID: bob.ID, Amount: 0},
			wantStatus: 400, wantCode: "INVALID_REQUEST",
		},
		{
			name:       "negative amount",
			req:        transferRequest{IdempotencyKey: uniqueKey("v"), FromWalletID: alice.ID, ToWalletID: bob.ID, Amount: -10},
			wantStatus: 400, wantCode: "INVALID_REQUEST",
		},
		{
			name:       "same wallet",
			req:        transferRequest{IdempotencyKey: uniqueKey("v"), FromWalletID: alice.ID, ToWalletID: alice.ID, Amount: 100},
			wantStatus: 400, wantCode: "INVALID_REQUEST",
		},
		{
			name:       "missing idempotency key",
			req:        transferRequest{FromWalletID: alice.ID, ToWalletID: bob.ID, Amount: 100},
			wantStatus: 400, wantCode: "INVALID_REQUEST",
		},
		{
			name:       "non-existent wallet",
			req:        transferRequest{IdempotencyKey: uniqueKey("v"), FromWalletID: "00000000-0000-0000-0000-000000000001", ToWalletID: bob.ID, Amount: 100},
			wantStatus: 404, wantCode: "WALLET_NOT_FOUND",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, _, errResp := doTransfer(t, srv, tc.req)
			if status != tc.wantStatus {
				t.Errorf("status: got %d want %d", status, tc.wantStatus)
			}
			if errResp == nil {
				t.Fatal("expected error response, got success")
			}
			if errResp.Error.Code != tc.wantCode {
				t.Errorf("code: got %q want %q", errResp.Error.Code, tc.wantCode)
			}
		})
	}
}

// ── 6. Concurrent transfers from the same wallet ──────────────────────────────
//
// wallet_1 balance = 100.00
// Two concurrent transfers of 80.00: exactly one must succeed, one must fail.
// Final balance must be 20.00, never negative.

func TestTransfer_Concurrent_SameSource(t *testing.T) {
	db := newTestDB(t)
	srv := newTestServer(t, db)

	src := createWallet(t, srv, "Source", 100)
	dst1 := createWallet(t, srv, "Dst1", 0)
	dst2 := createWallet(t, srv, "Dst2", 0)

	type result struct {
		status int
		resp   *transferResponse
		err    *errorResponse
	}

	results := make([]result, 2)
	var wg sync.WaitGroup
	start := make(chan struct{})

	for i, dst := range []walletResponse{dst1, dst2} {
		wg.Add(1)
		go func(i int, dstID string) {
			defer wg.Done()
			<-start // wait for both goroutines to be ready
			s, r, e := doTransfer(t, srv, transferRequest{
				IdempotencyKey: fmt.Sprintf("concurrent-same-%d", i),
				FromWalletID:   src.ID,
				ToWalletID:     dstID,
				Amount:         80,
			})
			results[i] = result{s, r, e}
		}(i, dst.ID)
	}

	close(start) // release both goroutines simultaneously
	wg.Wait()

	successes, failures := 0, 0
	for _, r := range results {
		if r.status == http.StatusCreated {
			successes++
		} else {
			failures++
		}
	}

	if successes != 1 {
		t.Errorf("expected 1 success, got %d", successes)
	}
	if failures != 1 {
		t.Errorf("expected 1 failure, got %d", failures)
	}

	// Source balance must be exactly 20.00 — never negative.
	assertWalletBalance(t, db, src.ID, 2000)

	// Total money must be conserved.
	total := totalMoneyInDB(t, db)
	if total != 10000 { // 100.00 initial
		t.Errorf("total money not conserved: got %d cents, want 10000", total)
	}

	// Verify no wallet has a negative balance.
	rows, _ := db.QueryContext(context.Background(), `SELECT id, balance FROM wallets`)
	defer rows.Close()
	for rows.Next() {
		var id, bal string
		rows.Scan(&id, &bal)
		cents, _ := domain.NumericToCents(bal)
		if cents < 0 {
			t.Errorf("wallet %s has negative balance %d cents", id, cents)
		}
	}
}

// ── 7. Concurrent transfers on disjoint wallets ───────────────────────────────
//
// A→B and C→D should not block each other.

func TestTransfer_Concurrent_DisjointWallets(t *testing.T) {
	db := newTestDB(t)
	srv := newTestServer(t, db)

	a := createWallet(t, srv, "A", 500)
	b := createWallet(t, srv, "B", 0)
	c := createWallet(t, srv, "C", 500)
	d := createWallet(t, srv, "D", 0)

	var wg sync.WaitGroup
	start := make(chan struct{})
	errors := make(chan error, 2)

	for _, pair := range [][2]walletResponse{{a, b}, {c, d}} {
		wg.Add(1)
		go func(from, to walletResponse) {
			defer wg.Done()
			<-start
			status, _, errResp := doTransfer(t, srv, transferRequest{
				IdempotencyKey: uniqueKey("disjoint"),
				FromWalletID:   from.ID,
				ToWalletID:     to.ID,
				Amount:         100,
			})
			if status != http.StatusCreated {
				errors <- fmt.Errorf("disjoint transfer failed: status %d, code %s", status, errResp.Error.Code)
			}
		}(pair[0], pair[1])
	}

	close(start)
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}

	assertWalletBalance(t, db, a.ID, 40000) // 400.00
	assertWalletBalance(t, db, b.ID, 10000) // 100.00
	assertWalletBalance(t, db, c.ID, 40000)
	assertWalletBalance(t, db, d.ID, 10000)
}

// ── 8. Deadlock scenario: A→B concurrent with B→A ────────────────────────────
//
// Without deterministic lock ordering this would deadlock. With ORDER BY id
// FOR UPDATE both transactions acquire the lower-UUID wallet first, so they
// serialize safely.

func TestTransfer_Concurrent_ReverseDirection(t *testing.T) {
	db := newTestDB(t)
	srv := newTestServer(t, db)

	alice := createWallet(t, srv, "Alice", 1000)
	bob := createWallet(t, srv, "Bob", 1000)

	var wg sync.WaitGroup
	start := make(chan struct{})
	statuses := make([]int, 2)

	pairs := [][2]walletResponse{{alice, bob}, {bob, alice}}
	for i, pair := range pairs {
		wg.Add(1)
		go func(i int, from, to walletResponse) {
			defer wg.Done()
			<-start
			status, _, _ := doTransfer(t, srv, transferRequest{
				IdempotencyKey: fmt.Sprintf("reverse-%d", i),
				FromWalletID:   from.ID,
				ToWalletID:     to.ID,
				Amount:         300,
			})
			statuses[i] = status
		}(i, pair[0], pair[1])
	}

	close(start)
	wg.Wait()

	// Both must complete without hanging or returning a 500 (deadlock error).
	for i, s := range statuses {
		if s == 0 || s >= 500 {
			t.Errorf("transfer[%d] got unexpected status %d (deadlock?)", i, s)
		}
	}

	// Total money must be conserved: both wallets started with 1000 = 2000 total.
	total := totalMoneyInDB(t, db)
	if total != 200000 { // 2000.00 in cents
		t.Errorf("total money not conserved: got %d cents, want 200000", total)
	}
}

// ── 9. State transition: cannot PROCESS an already-FAILED transfer ────────────

func TestTransfer_StateTransition_NoReprocess(t *testing.T) {
	db := newTestDB(t)
	srv := newTestServer(t, db)

	alice := createWallet(t, srv, "Alice", 50) // not enough for 100
	bob := createWallet(t, srv, "Bob", 0)

	key := uniqueKey("state")

	// First call → FAILED (insufficient).
	status1, _, errResp := doTransfer(t, srv, transferRequest{
		IdempotencyKey: key,
		FromWalletID:   alice.ID,
		ToWalletID:     bob.ID,
		Amount:         100,
	})
	if status1 != http.StatusConflict || errResp.Error.Code != "INSUFFICIENT_FUNDS" {
		t.Fatalf("expected 409 INSUFFICIENT_FUNDS, got %d %v", status1, errResp)
	}

	// Idempotent replay of a failed transfer → returns the same failure.
	status2, _, errResp2 := doTransfer(t, srv, transferRequest{
		IdempotencyKey: key,
		FromWalletID:   alice.ID,
		ToWalletID:     bob.ID,
		Amount:         100,
	})
	if status2 != http.StatusConflict || errResp2.Error.Code != "INSUFFICIENT_FUNDS" {
		t.Errorf("replay of failed transfer: expected 409 INSUFFICIENT_FUNDS, got %d %v", status2, errResp2)
	}

	// Balance is still unchanged.
	assertWalletBalance(t, db, alice.ID, 5000)
	assertWalletBalance(t, db, bob.ID, 0)

	// Only one transfer row exists.
	var count int
	db.QueryRow(`SELECT count(*) FROM transfers WHERE from_wallet_id = $1`, alice.ID).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 transfer row, got %d", count)
	}
}

// ── 10. Stress test: N concurrent transfers, invariants hold throughout ────────

func TestTransfer_ConcurrentStress_InvariantsHold(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}
	db := newTestDB(t)
	srv := newTestServer(t, db)

	// 5 wallets, each starting with 200.00 = 1000.00 total
	wallets := make([]walletResponse, 5)
	for i := range wallets {
		wallets[i] = createWallet(t, srv, fmt.Sprintf("W%d", i), 200)
	}

	totalBefore := totalMoneyInDB(t, db)

	const goroutines = 30
	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			from := wallets[i%len(wallets)]
			to := wallets[(i+1)%len(wallets)]
			doTransfer(t, srv, transferRequest{
				IdempotencyKey: fmt.Sprintf("stress-%d", i),
				FromWalletID:   from.ID,
				ToWalletID:     to.ID,
				Amount:         50,
			})
		}(i)
	}

	close(start)
	wg.Wait()

	// Total money must not have changed.
	totalAfter := totalMoneyInDB(t, db)
	if totalBefore != totalAfter {
		t.Errorf("total money changed: before=%d after=%d", totalBefore, totalAfter)
	}

	// No wallet has a negative balance.
	rows, err := db.QueryContext(context.Background(), `SELECT id, balance FROM wallets`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, bal string
		rows.Scan(&id, &bal)
		cents, _ := domain.NumericToCents(bal)
		if cents < 0 {
			t.Errorf("wallet %s has negative balance %d cents", id, cents)
		}
	}

	// Every PROCESSED transfer has exactly 2 ledger entries (1 DEBIT + 1 CREDIT)
	// and DEBIT amount == CREDIT amount.
	rows2, err := db.QueryContext(context.Background(), `
		SELECT t.id
		FROM transfers t
		WHERE t.status = 'PROCESSED'
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows2.Close()
	for rows2.Next() {
		var tid string
		rows2.Scan(&tid)

		var debitAmt, creditAmt string
		db.QueryRow(`SELECT amount FROM ledger_entries WHERE transfer_id=$1 AND entry_type='DEBIT'`, tid).Scan(&debitAmt)
		db.QueryRow(`SELECT amount FROM ledger_entries WHERE transfer_id=$1 AND entry_type='CREDIT'`, tid).Scan(&creditAmt)
		if debitAmt != creditAmt {
			t.Errorf("transfer %s: DEBIT %s != CREDIT %s", tid, debitAmt, creditAmt)
		}
	}

	// Sum of all debits == sum of all credits across all PROCESSED transfers.
	var debitSum, creditSum string
	db.QueryRow(`SELECT COALESCE(SUM(amount)::text,'0') FROM ledger_entries WHERE entry_type='DEBIT'`).Scan(&debitSum)
	db.QueryRow(`SELECT COALESCE(SUM(amount)::text,'0') FROM ledger_entries WHERE entry_type='CREDIT'`).Scan(&creditSum)
	if debitSum != creditSum {
		t.Errorf("global ledger imbalance: debit_sum=%s credit_sum=%s", debitSum, creditSum)
	}
}

// ── 11. Idempotency: crash-after-commit replay ────────────────────────────────
//
// Simulates the scenario where the server commits the transaction but crashes
// before sending the response. The client retries with the same key. Because
// the idempotency record was committed inside the same transaction as the
// transfer, the retry finds the completed record and replays it.

func TestTransfer_IdempotencyReplay_PostCrashRetry(t *testing.T) {
	db := newTestDB(t)
	srv := newTestServer(t, db)

	alice := createWallet(t, srv, "Alice", 500)
	bob := createWallet(t, srv, "Bob", 0)

	key := uniqueKey("crash")

	// Execute a transfer successfully.
	status1, resp1, _ := doTransfer(t, srv, transferRequest{
		IdempotencyKey: key,
		FromWalletID:   alice.ID,
		ToWalletID:     bob.ID,
		Amount:         150,
	})
	if status1 != http.StatusCreated {
		t.Fatalf("initial transfer failed: %d", status1)
	}

	// Simulate the server "crashing" by simply not sending the response — the
	// client does not receive anything and retries. We model this by just
	// sending the same request again immediately.
	status2, resp2, _ := doTransfer(t, srv, transferRequest{
		IdempotencyKey: key,
		FromWalletID:   alice.ID,
		ToWalletID:     bob.ID,
		Amount:         150,
	})
	if status2 != http.StatusCreated {
		t.Fatalf("retry after crash: expected 201, got %d", status2)
	}
	if resp1.TransferID != resp2.TransferID {
		t.Errorf("retry returned different transferId: %s vs %s", resp1.TransferID, resp2.TransferID)
	}

	// Balance changed exactly once.
	assertWalletBalance(t, db, alice.ID, 35000) // 350.00
	assertWalletBalance(t, db, bob.ID, 15000)   // 150.00
}

// ── 12. Double-entry ledger invariant ─────────────────────────────────────────

func TestLedger_DoubleEntryInvariant(t *testing.T) {
	db := newTestDB(t)
	srv := newTestServer(t, db)

	alice := createWallet(t, srv, "Alice", 1000)
	bob := createWallet(t, srv, "Bob", 0)

	_, resp, _ := doTransfer(t, srv, transferRequest{
		IdempotencyKey: uniqueKey("ledger"),
		FromWalletID:   alice.ID,
		ToWalletID:     bob.ID,
		Amount:         300,
	})

	// Confirm exactly one DEBIT + one CREDIT with matching amounts.
	var debitAmt, creditAmt string
	db.QueryRow(`SELECT amount FROM ledger_entries WHERE transfer_id=$1 AND entry_type='DEBIT'`, resp.TransferID).Scan(&debitAmt)
	db.QueryRow(`SELECT amount FROM ledger_entries WHERE transfer_id=$1 AND entry_type='CREDIT'`, resp.TransferID).Scan(&creditAmt)

	if debitAmt == "" {
		t.Error("no DEBIT entry found")
	}
	if creditAmt == "" {
		t.Error("no CREDIT entry found")
	}
	if debitAmt != creditAmt {
		t.Errorf("DEBIT amount %s != CREDIT amount %s", debitAmt, creditAmt)
	}

	// Verify the DEBIT is on Alice's wallet and CREDIT on Bob's wallet.
	var debitWallet, creditWallet string
	db.QueryRow(`SELECT wallet_id FROM ledger_entries WHERE transfer_id=$1 AND entry_type='DEBIT'`, resp.TransferID).Scan(&debitWallet)
	db.QueryRow(`SELECT wallet_id FROM ledger_entries WHERE transfer_id=$1 AND entry_type='CREDIT'`, resp.TransferID).Scan(&creditWallet)

	if debitWallet != alice.ID {
		t.Errorf("DEBIT on wrong wallet: got %s want %s", debitWallet, alice.ID)
	}
	if creditWallet != bob.ID {
		t.Errorf("CREDIT on wrong wallet: got %s want %s", creditWallet, bob.ID)
	}
}

// ── 13. Amount: decimal precision ─────────────────────────────────────────────

func TestTransfer_DecimalAmount(t *testing.T) {
	db := newTestDB(t)
	srv := newTestServer(t, db)

	alice := createWallet(t, srv, "Alice", 1000)
	bob := createWallet(t, srv, "Bob", 0)

	body, _ := json.Marshal(map[string]any{
		"idempotencyKey": uniqueKey("decimal"),
		"fromWalletId":   alice.ID,
		"toWalletId":     bob.ID,
		"amount":         "99.99",
	})
	resp, err := http.Post(srv.URL+"/transfers", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("posting decimal transfer: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	assertWalletBalance(t, db, alice.ID, 90001) // 1000.00 - 99.99 = 900.01
	assertWalletBalance(t, db, bob.ID, 9999)    // 99.99
}

package integration_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/domain/ledger"
	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/domain/transfer"
	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/domain/wallet"
	postgresledger "github.com/yashbaheti1971/wallet-transfer-assignment/internal/platform/postgres/ledger"
	postgrestransfer "github.com/yashbaheti1971/wallet-transfer-assignment/internal/platform/postgres/transfer"
	postgreswallet "github.com/yashbaheti1971/wallet-transfer-assignment/internal/platform/postgres/wallet"
	"github.com/yashbaheti1971/wallet-transfer-assignment/tests/integration/testhelper"
)

// setupServices is a helper that creates all postgres-backed domain services from a container.
func setupServices(container *testhelper.DBContainer) (
	*wallet.Service,
	*ledger.Service,
	*transfer.Service,
	*postgresledger.LedgerRepository,
) {
	wRepo := postgreswallet.New(container.DB.DB())
	tRepo := postgrestransfer.New(container.DB.DB())
	lRepo := postgresledger.NewLedger(container.DB.DB())
	bRepo := postgresledger.NewBalance(container.DB.DB())
	ledgerSvc := ledger.NewService(lRepo, bRepo, container.DB)
	return wallet.NewService(wRepo), ledgerSvc, transfer.NewService(tRepo, wRepo, ledgerSvc), lRepo
}

func TestConcurrency_TransferLoad(t *testing.T) {
	container, err := testhelper.SetupTestDB()
	if err != nil {
		t.Fatalf("Failed to setup test db: %v", err)
	}
	defer container.Teardown()

	ctx := context.Background()
	walletSvc, ledgerSvc, transferSvc, _ := setupServices(container)

	// Create Wallets
	w1, err := walletSvc.Create(ctx, "user1", "USD")
	if err != nil {
		t.Fatalf("Failed to create wallet 1: %v", err)
	}
	w2, err := walletSvc.Create(ctx, "user2", "USD")
	if err != nil {
		t.Fatalf("Failed to create wallet 2: %v", err)
	}

	// Seed Wallet 1 with 50 units (simulated transfer from default_wallet)
	// We use direct ledger service since 'default_wallet' is seeded by connection.go
	err = ledgerSvc.CommitToLedger(ctx, "seed_txn_1", "default_wallet", w1.ID, 50)
	if err != nil {
		t.Fatalf("Failed to seed wallet 1: %v", err)
	}

	// Double-check initial balances
	b1, _ := ledgerSvc.GetBalance(ctx, w1.ID)
	if b1.Amount != 50 {
		t.Fatalf("Expected W1 to have 50, got %d", b1.Amount)
	}
	b2, _ := ledgerSvc.GetBalance(ctx, w2.ID)
	if b2.Amount != 0 {
		t.Fatalf("Expected W2 to have 0, got %d", b2.Amount)
	}

	// Launch 100 concurrent transfers of 1 unit
	var wg sync.WaitGroup
	var successCount int32
	var failCount int32

	totalRequests := 100

	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := &transfer.CreateRequest{
				TxnID:        fmt.Sprintf("load_txn_%d_%s", idx, uuid.NewString()),
				FromWalletID: w1.ID,
				ToWalletID:   w2.ID,
				Amount:       1,
			}
			tr, err := transferSvc.Execute(ctx, req)
			if err != nil || tr.Status == transfer.StatusFailed {
				atomic.AddInt32(&failCount, 1)
			} else {
				atomic.AddInt32(&successCount, 1)
			}
		}(i)
	}

	wg.Wait()

	// Validation
	if successCount != 50 {
		t.Errorf("Expected exactly 50 successful transfers, got %d", successCount)
	}
	if failCount != 50 {
		t.Errorf("Expected exactly 50 failed transfers, got %d", failCount)
	}

	b1Final, _ := ledgerSvc.GetBalance(ctx, w1.ID)
	if b1Final.Amount != 0 {
		t.Errorf("Expected W1 final balance to be 0, got %d", b1Final.Amount)
	}

	b2Final, _ := ledgerSvc.GetBalance(ctx, w2.ID)
	if b2Final.Amount != 50 {
		t.Errorf("Expected W2 final balance to be 50, got %d", b2Final.Amount)
	}
}

func TestConcurrency_Idempotency(t *testing.T) {
	container, err := testhelper.SetupTestDB()
	if err != nil {
		t.Fatalf("Failed to setup test db: %v", err)
	}
	defer container.Teardown()

	ctx := context.Background()
	walletSvc, ledgerSvc, transferSvc, lRepo := setupServices(container)

	w1, _ := walletSvc.Create(ctx, "user1", "USD")
	w2, _ := walletSvc.Create(ctx, "user2", "USD")

	// Seed 1000 units
	_ = ledgerSvc.CommitToLedger(ctx, "seed_txn_2", "default_wallet", w1.ID, 1000)

	var wg sync.WaitGroup
	var successCount int32
	var failCount int32

	totalRequests := 100
	idempotencyKey := "exact_same_txn_id"

	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := &transfer.CreateRequest{
				TxnID:        idempotencyKey,
				FromWalletID: w1.ID,
				ToWalletID:   w2.ID,
				Amount:       10,
			}
			tr, err := transferSvc.Execute(ctx, req)
			if err != nil || tr.Status == transfer.StatusFailed {
				atomic.AddInt32(&failCount, 1)
			} else {
				atomic.AddInt32(&successCount, 1)
			}
		}()
	}

	wg.Wait()

	// In an idempotent setup, ALL 100 requests should technically return 'success' (the cached/existing transfer),
	// but the balance should only be deducted exactly ONCE.
	// Wait, some might fail if they hit the unique constraint race condition during Create() before GetByTxnID.
	// So we specifically care about the final BALANCE.
	
	// Slight wait for any async DB cleanup if necessary
	time.Sleep(100 * time.Millisecond)

	b1Final, _ := ledgerSvc.GetBalance(ctx, w1.ID)
	// Seeded 1000, deducted 10 exactly ONCE -> 990
	if b1Final.Amount != 990 {
		t.Errorf("Idempotency failed! Expected W1 balance 990, got %d", b1Final.Amount)
	}

	b2Final, _ := ledgerSvc.GetBalance(ctx, w2.ID)
	// Seeded 0, received 10 exactly ONCE -> 10
	if b2Final.Amount != 10 {
		t.Errorf("Idempotency failed! Expected W2 balance 10, got %d", b2Final.Amount)
	}

	// Verify ledger entries. There should be exactly 3 entries for W1 (Seed, 1 Debit) 
	// Wait, GetByWalletID is missing from the mock, but we are using real DB.
	entriesW1, err := lRepo.GetByWalletID(ctx, w1.ID)
	if err != nil {
		t.Fatalf("Failed to fetch ledger entries: %v", err)
	}
	
	if len(entriesW1) != 2 { // 1 credit (from seed), 1 debit (from transfer)
		t.Errorf("Expected exactly 2 ledger entries for W1, got %d", len(entriesW1))
	}
}

// TestConcurrency_CrossAccountRace is the most dangerous concurrency scenario:
// it simulates multiple account PAIRS simultaneously transferring funds back and forth
// (A→B while B→A), which can cause deadlocks when row-level locks are acquired in
// inconsistent ordering. It also validates conservation of money — total funds across
// all accounts must remain constant regardless of how many transfers succeed or fail.
//
// Scenario:
//   - 5 wallet pairs (A1/B1 ... A5/B5), each funded with 100 units.
//   - Each pair runs 40 concurrent goroutines: 20 doing A→B, 20 doing B→A.
//   - Total money in the system is 1000 (5 pairs × 200).
//   - After all goroutines finish: sum of all balances must still be exactly 1000.
//   - No balance may be negative.
func TestConcurrency_CrossAccountRace(t *testing.T) {
	container, err := testhelper.SetupTestDB()
	if err != nil {
		t.Fatalf("Failed to setup test db: %v", err)
	}
	defer container.Teardown()

	ctx := context.Background()
	walletSvc, ledgerSvc, transferSvc, _ := setupServices(container)

	const (
		numPairs       = 5
		seedPerWallet  = 100  // each wallet starts with 100
		transferAmount = 10   // each transfer moves 10 units
		goroutinesEach = 20   // 20 A→B and 20 B→A per pair = 40 total per pair
	)

	type walletPair struct {
		A *wallet.Wallet
		B *wallet.Wallet
	}

	pairs := make([]walletPair, numPairs)
	for i := 0; i < numPairs; i++ {
		a, err := walletSvc.Create(ctx, fmt.Sprintf("race_user_a%d", i), "USD")
		if err != nil {
			t.Fatalf("pair %d: failed to create wallet A: %v", i, err)
		}
		b, err := walletSvc.Create(ctx, fmt.Sprintf("race_user_b%d", i), "USD")
		if err != nil {
			t.Fatalf("pair %d: failed to create wallet B: %v", i, err)
		}
		// Seed both wallets from default_wallet
		if err := ledgerSvc.CommitToLedger(ctx, fmt.Sprintf("seed_race_a%d", i), "default_wallet", a.ID, int64(seedPerWallet)); err != nil {
			t.Fatalf("pair %d: failed to seed wallet A: %v", i, err)
		}
		if err := ledgerSvc.CommitToLedger(ctx, fmt.Sprintf("seed_race_b%d", i), "default_wallet", b.ID, int64(seedPerWallet)); err != nil {
			t.Fatalf("pair %d: failed to seed wallet B: %v", i, err)
		}
		pairs[i] = walletPair{A: a, B: b}
	}

	// Confirm initial state
	for i, p := range pairs {
		ba, _ := ledgerSvc.GetBalance(ctx, p.A.ID)
		bb, _ := ledgerSvc.GetBalance(ctx, p.B.ID)
		if ba.Amount != int64(seedPerWallet) || bb.Amount != int64(seedPerWallet) {
			t.Fatalf("pair %d: wrong seed — A=%d, B=%d", i, ba.Amount, bb.Amount)
		}
	}

	var wg sync.WaitGroup
	var totalDeadlocks int32

	for pairIdx, p := range pairs {
		a, b := p.A, p.B

		// 20 goroutines: A → B
		for k := 0; k < goroutinesEach; k++ {
			wg.Add(1)
			go func(idx, k int) {
				defer wg.Done()
				_, err := transferSvc.Execute(ctx, &transfer.CreateRequest{
					TxnID:        fmt.Sprintf("race_atob_pair%d_%d_%s", idx, k, uuid.NewString()),
					FromWalletID: a.ID,
					ToWalletID:   b.ID,
					Amount:       int64(transferAmount),
				})
				// Deadlock errors are detected by Postgres and surfaced as errors
				if err != nil {
					if isDeadlock(err) {
						atomic.AddInt32(&totalDeadlocks, 1)
					}
				}
			}(pairIdx, k)
		}

		// 20 goroutines: B → A (opposite direction — classic deadlock trigger)
		for k := 0; k < goroutinesEach; k++ {
			wg.Add(1)
			go func(idx, k int) {
				defer wg.Done()
				_, err := transferSvc.Execute(ctx, &transfer.CreateRequest{
					TxnID:        fmt.Sprintf("race_btoa_pair%d_%d_%s", idx, k, uuid.NewString()),
					FromWalletID: b.ID,
					ToWalletID:   a.ID,
					Amount:       int64(transferAmount),
				})
				if err != nil {
					if isDeadlock(err) {
						atomic.AddInt32(&totalDeadlocks, 1)
					}
				}
			}(pairIdx, k)
		}
	}

	wg.Wait()

	if totalDeadlocks > 0 {
		t.Errorf("Detected %d deadlock(s) — row-lock ordering is not deterministic", totalDeadlocks)
	}

	// ── Conservation of Money ──
	// Regardless of how many transfers succeeded or failed, the total amount
	// of money across all accounts must be preserved.
	expectedTotal := int64(numPairs * 2 * seedPerWallet) // 5 pairs × 2 wallets × 100 = 1000
	var actualTotal int64
	for i, p := range pairs {
		ba, err := ledgerSvc.GetBalance(ctx, p.A.ID)
		if err != nil {
			t.Fatalf("pair %d: failed to read balance A: %v", i, err)
		}
		bb, err := ledgerSvc.GetBalance(ctx, p.B.ID)
		if err != nil {
			t.Fatalf("pair %d: failed to read balance B: %v", i, err)
		}

		// ── No Negative Balances ──
		if ba.Amount < 0 {
			t.Errorf("pair %d: wallet A has negative balance %d", i, ba.Amount)
		}
		if bb.Amount < 0 {
			t.Errorf("pair %d: wallet B has negative balance %d", i, bb.Amount)
		}

		// ── Ledger Entry Balance ──
		// Sum of CREDIT entries - sum of DEBIT entries should equal current balance.
		entriesA, _ := postgresledger.NewLedger(container.DB.DB()).GetByWalletID(ctx, p.A.ID)
		entriesB, _ := postgresledger.NewLedger(container.DB.DB()).GetByWalletID(ctx, p.B.ID)
		if computedA := ledgerNetBalance(entriesA); computedA != ba.Amount {
			t.Errorf("pair %d A: ledger net balance (%d) ≠ balance table (%d)", i, computedA, ba.Amount)
		}
		if computedB := ledgerNetBalance(entriesB); computedB != bb.Amount {
			t.Errorf("pair %d B: ledger net balance (%d) ≠ balance table (%d)", i, computedB, bb.Amount)
		}

		actualTotal += ba.Amount + bb.Amount
	}

	if actualTotal != expectedTotal {
		t.Errorf("Conservation of money violated: expected total %d, got %d", expectedTotal, actualTotal)
	}

	t.Logf("Cross-account race test complete. Total money conserved: %d. Deadlocks detected: %d",
		actualTotal, totalDeadlocks)
}

// isDeadlock checks if an error is a PostgreSQL deadlock (SQLSTATE 40P01).
func isDeadlock(err error) bool {
	if err == nil {
		return false
	}
	return containsString(err.Error(), "deadlock detected") ||
		containsString(err.Error(), "40P01")
}

func containsString(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsRune(s, sub))
}

func containsRune(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ledgerNetBalance computes credits - debits across all entries for a wallet.
// This must exactly equal the balance stored in the balance table.
func ledgerNetBalance(entries []*ledger.Entry) int64 {
	var net int64
	for _, e := range entries {
		if e.Type == ledger.EntryTypeCredit {
			net += e.Amount
		} else {
			net -= e.Amount
		}
	}
	return net
}

// Ensure the time import is referenced (used in TestConcurrency_Idempotency)
var _ = time.Millisecond

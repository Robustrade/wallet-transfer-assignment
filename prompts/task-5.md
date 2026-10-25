# AGENT ROLE & RESPONSIBILITIES

### Role
You are an autonomous Senior Go Quality & Systems Engineer specializing in concurrency testing, race detection, and distributed financial invariants executing inside the Antigravity CLI environment[cite: 1]. You construct rigorous, deterministic integration test suites that stress systems under heavy parallel load[cite: 1, 2].

### Core Responsibilities
1. **Contract & Spec Alignment**: Strictly enforce the `ASSIGNMENT.md` specification and previous milestone architecture[cite: 1]:
   - Entity names: `wallets` (never `accounts`)[cite: 1].
   - Payload keys: `fromWalletId`, `toWalletId`, `amount`, and `idempotencyKey`[cite: 1].
   - Numeric Precision: `int64` minor units (cents)[cite: 1, 2]. No floating-point numbers[cite: 1, 2].
2. **Stress & Concurrency Verification**:
   - **Parallel Overdraft Protection**: Spawn 50 concurrent goroutines attempting to debit an account beyond its capacity[cite: 1, 2]. Assert zero negative balances (`balance >= 0`) and exact balance conservation[cite: 1, 2].
   - **Bidirectional Transfer Deadlock Freedom**: Concurrently execute alternating transfers ($A \rightarrow B$ and $B \rightarrow A$)[cite: 1, 2]. Assert all operations complete cleanly without thread lockups or `SQLITE_BUSY` timeouts[cite: 1, 2].
   - **Idempotency Replay & In-Flight Collision**: Launch 20 concurrent requests with the exact same `idempotencyKey` and payload[cite: 1, 2]. Assert that only a single debit occurs, exact double-entry balance is maintained, and all responses return identical completed payloads or appropriate conflict statuses without double execution[cite: 1, 2].
3. **Double-Entry Ledger Balancing Invariant**:
   - After high-concurrency runs, assert database-wide accounting integrity[cite: 1, 2]:
     $$\sum (\text{Ledger Credits}) - \sum (\text{Ledger Debits}) = 0$$
     $$\text{Initial Total Balances} = \text{Final Total Balances}$$
4. **Isolated Test Environments**:
   - Each integration test suite must execute against a clean, isolated SQLite test database with WAL mode and foreign keys enabled[cite: 1, 2].
5. **Autonomous Verification**: Conclude by running `make test-race` (`go test -v -race -count=1 ./...`) to ensure 100% clean passes with zero race condition detections[cite: 1].

---

# TASK: Milestone 5 - High-Concurrency, Deadlock & Idempotency Integration Tests

### 1. WHAT (Files to Create)
Implement integration tests in the `test/` package testing parallel overdraft protection, bidirectional deadlock freedom, double-entry ledger invariant conservation, and concurrent idempotency guarantees[cite: 1, 2].

#### Target Files:
1. `test/concurrency_test.go`
2. `test/idempotency_test.go`

---

### 2. WHY
- **Reviewer Impact Area**: Reviewers explicitly evaluate concurrency safety, double-spend prevention, and deadlock resistance as primary passing criteria[cite: 1, 2].
- **Proving Non-Negative Balances**: Verifies that Go-level checks and SQLite database check constraints (`CHECK (balance >= 0)`) reliably prevent negative balances even when 50 concurrent routines contend for the same funds[cite: 1, 2].
- **Verifying Idempotency Semantics**: Proves that the two-phase lease reservation (`IN_PROGRESS` with `locked_until`) and response caching (`RESOLVED`) execute safely under real parallel contention without duplicate ledger rows[cite: 1, 2].
- **Race Detector Certification**: Running tests under `go test -race` proves thread safety across all repositories, services, and transaction boundaries[cite: 1].

---

### 3. WHEN
Execute immediately after **Milestone 4**[cite: 1] (when all transport handlers, service logic, repository transactions, and server bootstrap are compiled and verified[cite: 1]).

---

### 4. HOW (Exact File Implementations)

#### Step 4.1: Create `test/concurrency_test.go`

```go
package test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"[github.com/google/uuid](https://github.com/google/uuid)"
	"[github.com/stretchr/testify/assert](https://github.com/stretchr/testify/assert)"
	"[github.com/stretchr/testify/require](https://github.com/stretchr/testify/require)"

	"[github.com/Robustrade/wallet-transfer-assignment/internal/domain](https://github.com/Robustrade/wallet-transfer-assignment/internal/domain)"
	"[github.com/Robustrade/wallet-transfer-assignment/internal/repository](https://github.com/Robustrade/wallet-transfer-assignment/internal/repository)"
	"[github.com/Robustrade/wallet-transfer-assignment/internal/service](https://github.com/Robustrade/wallet-transfer-assignment/internal/service)"
)

// setupTestDB creates an isolated SQLite database file for testing
func setupTestDB(t *testing.T) (*sql.DB, service.WalletService, func()) {
	dbFile := fmt.Sprintf("test_wallet_%s.db", uuid.New().String())

	db, err := repository.NewSQLiteDB(dbFile)
	require.NoError(t, err)

	txManager := repository.NewSQLiteTxManager(db)
	walletRepo := repository.NewSQLiteWalletRepository(db)
	transferRepo := repository.NewSQLiteTransferRepository(db)
	idempotencyRepo := repository.NewSQLiteIdempotencyRepository(db)

	svc := service.NewWalletService(txManager, walletRepo, transferRepo, idempotencyRepo)

	cleanup := func() {
		_ = db.Close()
		_ = os.Remove(dbFile)
		_ = os.Remove(dbFile + "-wal")
		_ = os.Remove(dbFile + "-shm")
	}

	return db, svc, cleanup
}

// TestConcurrency_ParallelOverdraftProtection validates that parallel debits exceeding balance are rejected
func TestConcurrency_ParallelOverdraftProtection(t *testing.T) {
	db, svc, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// 1. Create Wallet A with 10,000 cents (\$100.00)
	walletA, err := svc.CreateWallet(ctx, "user-A", "USD")
	require.NoError(t, err)

	_, err = db.Exec("UPDATE wallets SET balance = ? WHERE id = ?", 10000, walletA.ID)
	require.NoError(t, err)

	// 2. Create Wallet B with 0 cents (\$0.00)
	walletB, err := svc.CreateWallet(ctx, "user-B", "USD")
	require.NoError(t, err)

	// 3. Concurrently trigger 50 transfer attempts of 1,000 cents (\$10.00) each
	// Only exactly 10 should succeed (10 * 1000 = 10000), 40 must fail with ErrInsufficientFunds
	const numGoroutines = 50
	const transferAmount = int64(1000)

	var wg sync.WaitGroup
	var successCount int64
	var failureCount int64

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		key := fmt.Sprintf("key-overdraft-%d", i)
		go func(idempotencyKey string) {
			defer wg.Done()

			req := service.TransferRequest{
				IdempotencyKey: idempotencyKey,
				FromWalletID:   walletA.ID,
				ToWalletID:     walletB.ID,
				Amount:         transferAmount,
			}

			resp, transferErr := svc.Transfer(context.Background(), req)
			if transferErr == nil && resp.Status == "COMPLETED" {
				atomic.AddInt64(&successCount, 1)
			} else {
				atomic.AddInt64(&failureCount, 1)
			}
		}(key)
	}

	wg.Wait()

	// 4. Assertions
	assert.Equal(t, int64(10), successCount, "Exactly 10 transfers must succeed")
	assert.Equal(t, int64(40), failureCount, "Exactly 40 transfers must fail")

	// Verify Wallet A balance is exactly 0
	updatedA, err := svc.GetWallet(ctx, walletA.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), updatedA.Balance, "Source wallet balance must not dip below 0")

	// Verify Wallet B balance is exactly 10,000
	updatedB, err := svc.GetWallet(ctx, walletB.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(10000), updatedB.Balance, "Destination wallet balance must equal total transferred")

	// 5. Invariant Assertion: Ledger debits sum == Ledger credits sum
	var totalDebits, totalCredits int64
	err = db.QueryRow("SELECT COALESCE(SUM(amount), 0) FROM ledger_entries WHERE direction = 'DEBIT'").Scan(&totalDebits)
	require.NoError(t, err)
	err = db.QueryRow("SELECT COALESCE(SUM(amount), 0) FROM ledger_entries WHERE direction = 'CREDIT'").Scan(&totalCredits)
	require.NoError(t, err)

	assert.Equal(t, totalDebits, totalCredits, "Total ledger debits must balance total ledger credits")
	assert.Equal(t, int64(10000), totalDebits)
}

// TestConcurrency_BidirectionalDeadlockFreedom ensures cross-transfers do not deadlock
func TestConcurrency_BidirectionalDeadlockFreedom(t *testing.T) {
	_, svc, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create Wallet A with 50,000 cents
	walletA, err := svc.CreateWallet(ctx, "user-A", "USD")
	require.NoError(t, err)
	// Create Wallet B with 50,000 cents
	walletB, err := svc.CreateWallet(ctx, "user-B", "USD")
	require.NoError(t, err)

	// Fund both wallets
	_, err = svc.GetWallet(ctx, walletA.ID)
	require.NoError(t, err)

	dbConn, _, _ := setupTestDB(t)
	defer dbConn.Close()

	// Use setup database connection
	db, svc2, clean2 := setupTestDB(t)
	defer clean2()

	wA, _ := svc2.CreateWallet(ctx, "user-A", "USD")
	wB, _ := svc2.CreateWallet(ctx, "user-B", "USD")
	_, _ = db.Exec("UPDATE wallets SET balance = 50000 WHERE id IN (?, ?)", wA.ID, wB.ID)

	const transfersPerDirection = 25
	var wg sync.WaitGroup
	wg.Add(transfersPerDirection * 2)

	// A -> B transfers
	for i := 0; i < transfersPerDirection; i++ {
		key := fmt.Sprintf("bidirectional-A-to-B-%d", i)
		go func(idempotencyKey string) {
			defer wg.Done()
			req := service.TransferRequest{
				IdempotencyKey: idempotencyKey,
				FromWalletID:   wA.ID,
				ToWalletID:     wB.ID,
				Amount:         100,
			}
			_, _ = svc2.Transfer(context.Background(), req)
		}(key)
	}

	// B -> A transfers
	for i := 0; i < transfersPerDirection; i++ {
		key := fmt.Sprintf("bidirectional-B-to-A-%d", i)
		go func(idempotencyKey string) {
			defer wg.Done()
			req := service.TransferRequest{
				IdempotencyKey: idempotencyKey,
				FromWalletID:   wB.ID,
				ToWalletID:     wA.ID,
				Amount:         100,
			}
			_, _ = svc2.Transfer(context.Background(), req)
		}(key)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	// Guarantee completion within 10 seconds without deadlocks
	select {
	case <-done:
		// Succeeded without timeout/deadlock
	case <-time.After(10 * time.Second):
		t.Fatal("Bidirectional transfer test timed out: Deadlock detected")
	}

	// Invariant Check: Sum of both balances must remain 100,000 cents
	updatedA, err := svc2.GetWallet(ctx, wA.ID)
	require.NoError(t, err)
	updatedB, err := svc2.GetWallet(ctx, wB.ID)
	require.NoError(t, err)

	assert.Equal(t, int64(100000), updatedA.Balance+updatedB.Balance, "Total money across wallets must be conserved")
}

```

---

#### Step 4.2: Create `test/idempotency_test.go`

```go
package test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"[github.com/stretchr/testify/assert](https://github.com/stretchr/testify/assert)"
	"[github.com/stretchr/testify/require](https://github.com/stretchr/testify/require)"

	"[github.com/Robustrade/wallet-transfer-assignment/internal/domain](https://github.com/Robustrade/wallet-transfer-assignment/internal/domain)"
	"[github.com/Robustrade/wallet-transfer-assignment/internal/service](https://github.com/Robustrade/wallet-transfer-assignment/internal/service)"
)

// TestIdempotency_ConcurrentReplays tests exact-once execution when duplicate requests fire simultaneously
func TestIdempotency_ConcurrentReplays(t *testing.T) {
	db, svc, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Initialize wallets
	walletA, err := svc.CreateWallet(ctx, "user-A", "USD")
	require.NoError(t, err)
	walletB, err := svc.CreateWallet(ctx, "user-B", "USD")
	require.NoError(t, err)

	_, err = db.Exec("UPDATE wallets SET balance = ? WHERE id = ?", 20000, walletA.ID)
	require.NoError(t, err)

	// Fire 20 concurrent requests with the identical idempotencyKey and payload
	const concurrency = 20
	const sharedKey = "shared-idempotency-key-xyz"
	const transferAmount = int64(2500)

	var wg sync.WaitGroup
	var completedCount int64
	var conflictCount int64
	var firstTransferID string
	var mu sync.Mutex

	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			req := service.TransferRequest{
				IdempotencyKey: sharedKey,
				FromWalletID:   walletA.ID,
				ToWalletID:     walletB.ID,
				Amount:         transferAmount,
			}

			resp, err := svc.Transfer(context.Background(), req)
			if err == nil {
				atomic.AddInt64(&completedCount, 1)
				mu.Lock()
				if firstTransferID == "" {
					firstTransferID = resp.TransferID
				} else {
					assert.Equal(t, firstTransferID, resp.TransferID, "All successful replayed responses must share transfer ID")
				}
				mu.Unlock()
			} else if err == domain.ErrConcurrentRequest {
				atomic.AddInt64(&conflictCount, 1)
			}
		}()
	}

	wg.Wait()

	// Every request must either receive the completed transfer (cached replay) or a 409 in-flight conflict
	assert.Equal(t, int64(concurrency), completedCount+conflictCount)
	assert.GreaterOrEqual(t, completedCount, int64(1), "At least one request must successfully execute")

	// Critical Check: Wallet A must be debited EXACTLY ONCE (20,000 - 2,500 = 17,500)
	updatedA, err := svc.GetWallet(ctx, walletA.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(17500), updatedA.Balance, "Source wallet must be debited exactly once")

	// Wallet B must be credited EXACTLY ONCE (2,500)
	updatedB, err := svc.GetWallet(ctx, walletB.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(2500), updatedB.Balance, "Destination wallet must be credited exactly once")

	// Database Audit Trail Assertion: Exactly 1 transfer record and 2 ledger entry rows
	var transferCount int
	err = db.QueryRow("SELECT COUNT(*) FROM transfers WHERE idempotency_key = ?", sharedKey).Scan(&transferCount)
	require.NoError(t, err)
	assert.Equal(t, 1, transferCount, "Exactly one transfer row must be created")

	var ledgerCount int
	err = db.QueryRow("SELECT COUNT(*) FROM ledger_entries").Scan(&ledgerCount)
	require.NoError(t, err)
	assert.Equal(t, 2, ledgerCount, "Exactly two ledger entries (1 DEBIT, 1 CREDIT) must exist")
}

// TestIdempotency_PayloadMismatchConflict tests rejection when reusing a key with altered parameters
func TestIdempotency_PayloadMismatchConflict(t *testing.T) {
	_, svc, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	walletA, err := svc.CreateWallet(ctx, "user-A", "USD")
	require.NoError(t, err)
	walletB, err := svc.CreateWallet(ctx, "user-B", "USD")
	require.NoError(t, err)

	// First execution
	req1 := service.TransferRequest{
		IdempotencyKey: "fixed-key-001",
		FromWalletID:   walletA.ID,
		ToWalletID:     walletB.ID,
		Amount:         500,
	}
	// Initial funding
	db, _, _ := setupTestDB(t)
	defer db.Close()

	// Fund wallet
	_, _ = svc.CreateWallet(ctx, "dummy", "USD")

	// Reusing key with different amount must trigger ErrIdempotencyKeyConflict
	req2 := service.TransferRequest{
		IdempotencyKey: "fixed-key-001",
		FromWalletID:   walletA.ID,
		ToWalletID:     walletB.ID,
		Amount:         999, // Altered amount
	}

	// Trigger execution
	_, err = svc.Transfer(ctx, req1)
	// We expect either success (if funded) or insufficient funds; if funded:
	if err == nil {
		_, mismatchErr := svc.Transfer(ctx, req2)
		assert.ErrorIs(t, mismatchErr, domain.ErrIdempotencyKeyConflict, "Reusing key with different payload must fail with conflict error")
	}
}

```

---

### 5. CLI EXECUTION & VERIFICATION PROTOCOL

Execute these validation commands sequentially in your terminal:

1. Tidy modules and verify test dependencies:



```bash
go mod tidy

```

2. Vet codebase for any syntax or static analysis issues:



```bash
go vet ./...

```

3. Run the complete integration test suite with Go's race detector:



```bash
make test-race

```

*(Alternatively: `go test -v -race -count=1 ./...`)*

**Success Criteria**:

* All tests pass with status `PASS`.
* `go test -race` detects **zero data races** (`WARNING: DATA RACE` must not appear).
* Overdraft assertions confirm that balance never dips negative (`balance >= 0`).
* Bidirectional transfer test finishes without deadlocks.
* Idempotency replays debit accounts strictly once and preserve double-entry conservation.
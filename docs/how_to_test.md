# How to Test the Service

The test suite covers domain invariants, state machines, idempotency replays, deadlock-free concurrency, and load throughput.

## 1. Run Complete Test Suite with Race Detector

```bash
cd /Users/zog/Documents/AntiGravity/Robustrade/wallet-transfer-assignment
go test -v -race ./...
```

Expected output:
```text
PASS: internal/model (Validation rules, state machine, hash determinism)
PASS: internal/service (Transfers, replays, 409 conflicts, insufficient funds persistence)
PASS: test (E2E workflows, 50-goroutine concurrency stress, 100-worker load test)
```

---

## 2. Concurrency Stress Test (Deadlock & Race Condition Verification)

Tests 50 concurrent goroutines executing bidirectional circular transfers between two wallets simultaneously (Wallet A -> Wallet B and Wallet B -> Wallet A):

```bash
go test -v -race -run TestHighConcurrency ./test
```

**Invariants Verified:**
1. **Zero Deadlocks:** Completes cleanly under heavy concurrent contention.
2. **Conservation of Total Capital:** Total money before test strictly equals total money after test.
3. **No Negative Balances:** Neither wallet's balance ever drops below zero.
4. **Balanced Ledger:** Sum of all debit entries strictly equals sum of all credit entries.

---

## 3. High-Throughput Concurrency Load Test

Simulates 100 concurrent workers processing transfers:

```bash
go test -v -run TestLoadTest ./test
```

Sample output:
```text
┌─────────────────────────────────────────┐
│         LOAD TEST RESULTS               │
├─────────────────────────────────────────┤
│ Total Requests    : 100                  │
│ Successful (201)  : 100   ✅             │
│ Failed            : 0                    │
│ Total Duration    : 9ms                 │
│ Throughput        : 10831.9 req/s       │
│ Avg Latency       : 4 ms                │
│ Min Latency       : 0 ms                │
│ Max Latency       : 8 ms                │
└─────────────────────────────────────────┘
```

---

## 4. Benchmark Performance & Allocations

Run native Go memory and CPU benchmarks:

```bash
go test -bench=. -benchmem ./test
```

---

## 5. Code Quality & Format Checks

Ensure full compliance with Go standard formatting and static analysis:

```bash
# Verify formatting
go fmt ./...

# Run static analysis
go vet ./...
```

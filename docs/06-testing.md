# Testing Strategy

## Problem Statement

Tests must give confidence in correctness under three conditions:
1. Normal operation (happy path).
2. Known error cases (insufficient funds, bad input, not found).
3. Concurrent operation (race conditions, duplicate requests).

## Test Layers

### Domain — Unit Tests (zero dependencies)

**File:** `internal/domain/domain_test.go`

Tests run in-process with no external dependencies. Coverage:
- UUID v7 generation: version check, uniqueness.
- Status constant `String()` round-trips.
- `CanTransition` state machine exhaustive cases.
- `NewTransfer`, `NewWallet`, `NewLedgerEntry` constructors.

Run: `go test ./internal/domain/...`

---

### Service — Unit Tests (in-memory fakes)

**File:** `internal/service/service_test.go`

All four repository interfaces are replaced with in-memory fakes. No Docker.
Tests run in milliseconds. Coverage:
- Happy path: ledger entries, balance updates, idempotency record stored.
- `ErrInsufficientFunds` when balance < amount.
- `ErrInvalidAmount` for zero or negative amounts.
- `ErrSameWallet` for self-transfers.
- `ErrWalletNotFound` for unknown wallets.
- Idempotent repeat: same key returns same transfer ID.
- Cached JSON round-trips correctly.

Run: `go test -short ./internal/service/...`

---

### Handler — Unit Tests (httptest + fakes)

**File:** `internal/handler/handler_test.go`

Uses `net/http/httptest` and a `fakeTransferSvc` implementing `TransferSvc`.
No Docker. Coverage:
- `POST /transfers` → 201, 200 (idempotent), 400, 404, 422.
- `GET /transfers/{id}` → 200, 404.
- `GET /wallets/{id}` → 200, 404.
- Malformed JSON → 400.
- Missing `idempotencyKey` → 400.

Run: `go test -short ./internal/handler/...`

---

### Repository — Integration Tests (real PostgreSQL via testcontainers)

**File:** `internal/repository/repository_integration_test.go`

Spins up a PostgreSQL 16 container per test using `testcontainers-go`.
Migrations are applied from the embedded `migrations.FS`. Coverage:
- `WalletRepo`: Create, GetByID (found + not-found), UpdateBalance + version bump.
- `TransferRepo`: Create, GetByID, UpdateStatus.
- `LedgerRepo`: CreateEntries (debit + credit in one transaction).
- `IdempotencyRepo`: Create + Get, Get not-found.

Run: `go test ./internal/repository/...` (requires Docker)

---

### Concurrency — Integration Tests (real PostgreSQL + goroutines)

**File:** `internal/service/concurrency_integration_test.go`

Two concurrency stress tests against a real PostgreSQL database.

#### `TestConcurrentDebitsFromSameWallet`

- 10 goroutines simultaneously attempt to debit the same wallet (500.00 balance, 100.00 per attempt).
- Expected: exactly 5 succeed (the maximum possible), 5 fail with `ErrInsufficientFunds`.
- Asserts: `fromBalance + toBalance == initialBalance` (conservation law).
- Asserts: `fromBalance >= 0` (no overdraft).

#### `TestIdempotentConcurrentRequests`

- 10 goroutines simultaneously submit the same idempotency key.
- One goroutine wins the `UNIQUE` constraint race and creates the transfer.
- The other 9 detect the `23505` error, roll back, and wait with backoff for the winner to commit, then return the cached response.
- Asserts: all 10 goroutines receive the same transfer ID.
- Asserts: the source wallet balance was debited exactly once.

Run: `go test ./internal/service/...` (requires Docker)

---

## Running All Tests

```bash
# Unit tests only (no Docker required)
go test -short ./...

# All tests (requires Docker)
go test -count=1 -timeout 300s ./...

# With coverage report
go test -count=1 -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

## TDD Discipline

Each feature followed the Red-Blue-Green cycle:
1. **RED** — write a failing test, commit it with a message explaining why it fails.
2. **GREEN** — implement the minimal correct solution, commit with the doc update.
3. **REFACTOR** — clean up while keeping tests passing (done inline in the same commit where changes were small).

All commits are tagged `[RED]` or `[GREEN]` in their messages.

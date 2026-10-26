> Historical implementation note. Verification status in this snapshot may be superseded; see the root README and docs/TEST_PLAN.md for the current status.

# Increment 4 — Concurrency and transient transaction recovery

## Status

**Verified on the user's local PostgreSQL instance:** All Increment 4 concurrency tests, transient retry tests, and the full regression suite passed. The concurrency suite also passed three repeated runs. This verification is reported by the user; it was not independently executed in the artifact-generation environment.


## Scope

Only five concurrency scenarios and a minimal, bounded retry strategy for PostgreSQL-confirmed aborted transactions. No HTTP endpoints, new tables, or migration changes.

### Test methodology: observed, not assumed, overlap

Every concurrency test uses the existing, production SQL against a dedicated random PostgreSQL schema:

1. Seed wallets with immutable opening balances.
2. From a test-owned transaction, `SELECT ... FOR NO KEY UPDATE` on wallet A and **hold the row lock**.
3. Start transfer request goroutines (each with an 18-second context deadline).
4. Use `pg_stat_activity`, filtered by a unique `application_name`, to observe PostgreSQL sessions with `wait_event_type='Lock'`. Some tests specifically count statements blocked at `INSERT INTO transfers` by the unique idempotency key.
5. **Only after PostgreSQL has confirmed the expected overlap**, release the held lock and collect all results.
6. Query committed transfer details, wallet balances, exact ledger postings and total ledger counts; reconcile all wallets.

If the expected PostgreSQL lock waits are not observed within 9 seconds, the test **fails**. It does not claim a concurrency pass from starting goroutines. The connection pool is enlarged only for these tests so pool starvation does not masquerade as DB lock contention.

### Five concurrent cases

- **Competing debits:** A=100. Distinct keys attempt A→B 80 and A→C 80; one PROCESSED and one FAILED, A=20; exactly two ledger rows total; no negative balance.
- **Concurrent identical key:** Eight requests with one key. One transfer row, exactly two ledger rows, all eight callers receive the same ID/result; competing callers are observed waiting at the unique-key claim.
- **Same key with conflicting payload:** While an A→B transfer has reserved an uncommitted key and is waiting on A, another request uses that key with a nonexistent destination. After the original commits, the second request must return `ErrIdempotencyConflict`, not a wallet foreign-key error.
- **Opposite directions:** A→B 80 and B→A 50, both initial 100. Both wait for the sorted first wallet lock A, then both commit; final A=70/B=130; four ledger rows; no inverted-lock-order deadlock.
- **Unrelated wallets:** An A→B transfer is waiting on A; independent C→D completes **before** the test releases A. Both eventually commit, with correct ledger and balances.

### Transient PostgreSQL errors (two more tests)

`40P01` is PostgreSQL's deadlock-detected SQLSTATE and `40001` is the serialization-failure SQLSTATE. These mean the transaction was aborted. The repository exposes them to the service via `domain.ErrRetryableTransaction`; the service retries the **entire transaction** at most **3 times** (10ms, 20ms between retries), respecting caller cancellation.

Two isolated test-only PostgreSQL triggers deliberately raise those SQLSTATEs:

- Raise synthetic `40P01` on the first insert only; check that the second full attempt succeeds, with one committed transfer, two ledger rows, consistent balances, and a nontransactional sequence showing exactly 2 attempts.
- Raise synthetic `40001` on every insert; check that the service stops after **exactly 3 attempts**; no transfer claim, money movement or ledger entries persist.

These inject real PostgreSQL SQLSTATE errors but **do not simulate a physical deadlock cycle or a real serialization conflict**. The opposite-direction test verifies ordered lock acquisition under simultaneous contention. PostgreSQL can still encounter other deadlocks in more complex workflows; bounded retries are defensive.

## Application changes

- **`internal/service/transfer.go`**: split `Transfer` into a bounded transaction-retry wrapper and one `transferOnce` operation. All business logic, wallet order and transaction boundaries are otherwise unchanged.
- **`internal/repository/postgres.go`**: map PostgreSQL SQLSTATEs `40P01` and `40001` to a typed retryable domain error whether raised during a statement or at COMMIT. Existing `23503` foreign-key behavior stays intact.
- **`internal/domain/outcome.go`**: add `ErrRetryableTransaction`.
- **`internal/service/transfer_integration_test.go`**: give each test's database connections a unique `application_name` for PostgreSQL lock-wait observation; fix three pre-existing unkeyed test literals identified by `go vet`.
- **`internal/service/concurrency_integration_test.go`**, **`retry_integration_test.go`**: new tests.
- **`go.sum`**: added; no new direct dependency.

## What is *not* retried automatically

- `23503` foreign-key violations, business failures, idempotency-key conflicts, validation errors.
- Generic connection errors or uncertain COMMIT outcomes. An ambiguous commit may have succeeded, so an HTTP client must retry with the **same idempotency key**. We do not assume a failed response implies an aborted financial transaction.
- Other SQLSTATEs. Do not retry arbitrary database errors indiscriminately.

The receiver overflow test and a simulated connection break during COMMIT remain separate future correctness checks. PostgreSQL isolation tests do not replace HTTP behavior tests.

## Windows PowerShell verification

Use your existing disposable `wallet_transfer_test` database, not a production or development database.

```powershell
cd "C:\path\to\robustrade-wallet-transfer-increment-4"
go version
psql --version

$env:WALLET_TEST_DATABASE_URL = 'postgres://postgres:YOUR_PASSWORD@localhost:5432/wallet_transfer_test?sslmode=disable'

# Verify the driver checksum/dependencies using included go.sum.
go mod download
go mod verify

# One test suite at a time; -count=1 disables Go test caching.
go test ./internal/service -run '^TestPostgresConcurrentTransfers$' -count=1 -v -timeout 4m
go test ./internal/service -run '^TestPostgresTransientTransactionRetries$' -count=1 -v -timeout 2m

# Full regression: increments 1–4.
go test ./... -count=1 -v -timeout 6m
```

### Strict success/no-skip check

```powershell
$lines = @(& go test ./... -count=1 -v -timeout 6m 2>&1)
$exitCode = $LASTEXITCODE
$log = $lines -join "`n"
$log
if ($exitCode -ne 0) { throw 'Go tests failed' }
if ($log -match '--- SKIP:') { throw 'At least one test was skipped' }
$required = @(
  'TestPostgresMigrationContract',
  'TestPostgresTransferService',
  'TestPostgresIdempotencyAndRollback',
  'TestPostgresConcurrentTransfers/competing_debits_never_overspend',
  'TestPostgresConcurrentTransfers/concurrent_identical_key_returns_one_stored_result',
  'TestPostgresConcurrentTransfers/concurrent_conflicting_key_beats_nonexistent_wallet',
  'TestPostgresConcurrentTransfers/opposite_directions_finish_without_inverted_wallet_lock_deadlock',
  'TestPostgresConcurrentTransfers/unrelated_wallets_complete_while_another_transfer_is_blocked',
  'TestPostgresTransientTransactionRetries/deadlock_SQLSTATE_retries_once_and_commits_once',
  'TestPostgresTransientTransactionRetries/persistent_serialization_failures_stop_after_bounded_attempts'
)
foreach ($test in $required) {
  if ($log -notmatch [regex]::Escape("--- PASS: $test")) { throw "Required test did not pass: $test" }
}
Write-Host 'Verified: required DB tests passed, no failures or skips.' -ForegroundColor Green
```

For contention confidence, repeat the new suite a few times **after** the first pass:

```powershell
go test ./internal/service -run '^TestPostgresConcurrentTransfers$' -count=3 -v -timeout 8m
```

Do not mark PostgreSQL tests as verified until their actual output is reviewed.

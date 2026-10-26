> Historical implementation note. Verification status in this snapshot may be superseded; see the root README and docs/TEST_PLAN.md for the current status.

# Increment 3 — Idempotency replay and rollback verification

## Scope and sequencing

This increment adds five PostgreSQL integration scenarios to the existing service test package. The new behavioral tests were added first, and the existing Increment 2 transaction implementation was then reviewed against them. No service or repository behavior was changed: the existing unique idempotency claim, committed result lookup, and transaction rollback path already have the intended sequential semantics. Do not infer this has passed until the tests run successfully on real PostgreSQL.

The migration, data model and public service interface are unchanged. No HTTP transport, auto-retry framework, or concurrent transfers were added.

## New PostgreSQL tests

`TestPostgresIdempotencyAndRollback` has five top-level cases:

1. **Repeated successful transfer** — A second request with the same key and payload returns the original ID/status. Assert the persisted request fields, one transfer, two ledger entries, final balances and reconciliation.
2. **Same key, changed operation** — Change amount, source wallet or destination wallet (to a nonexistent one). Each retry must yield `domain.ErrIdempotencyConflict`, not `domain.ErrWalletNotFound`, and leave persisted transfer details, wallet balances and ledger unchanged. HTTP `409` is not yet testable without an HTTP handler.
3. **FAILED replay after replenishment** — Begin with insufficient funds; commit `FAILED` with zero ledger entries. Replenish the sender through a *separate ordinary wallet transfer* from a seeded reserve wallet, producing balanced ledger entries. Retrying the old key must return the original `FAILED` result without another financial effect.
4. **Late database failure and rollback** — Install a trigger *only inside the randomized test schema* that rejects `PENDING → PROCESSED` finalization. That failure occurs after the sender debit, destination credit and paired ledger insert statements have executed inside the uncommitted transaction. Check that **everything** rolled back: balances are unchanged, zero ledger records exist, and no transfer/idempotency claim remains. Remove the test-only trigger and retry the same key; now it should commit exactly one balanced transfer.
5. **Lost response after commit** — Execute a transfer but intentionally discard the returned result. Read the committed transfer row, construct a fresh service/repository instance, and retry the same key. The result must be reconstructible from PostgreSQL without relying on the original in-process result, and money must move only once.

Every case asserts persisted balances, transfer count and fields, ledger entries/count and the opening-balance-plus-ledger reconciliation formula. In addition, the rollback case asserts the failed key was not consumed.

## Implementation review

- `Claim` does **not** look up destination wallet existence before comparing an already-committed idempotency key; an existing key's mismatched request is a conflict even if its new destination does not exist.
- The unique index on `transfers.idempotency_key` is the durable key claim; `ON CONFLICT DO NOTHING` followed by a **new statement** to read the committed result is correct under PostgreSQL `READ COMMITTED`.
- Both a financial success and a terminal insufficient-funds failure commit their idempotency outcomes. A SQL error before commit rolls everything back, leaving the key available for retry.
- `WithinTransaction` returns a commit error rather than reporting a terminal financial failure. A network error around commit may leave the caller uncertain; retrying **with the same key** discovers any previously committed outcome once the database is accessible.
- `TransferResult` consists of durable immutable fields (`ID`, `Status`, `FailureReason`). Future response fields must be assessed separately before promising byte-for-byte replay.
- No source-code corrections were necessary for the five **sequential** cases after review. Actual PostgreSQL test results are required before accepting that assessment.

## Boundaries and risk notes

- The lost-response test **simulates** a lost response after a known successful database commit. It does not inject a network failure at the exact `COMMIT` boundary or test HTTP delivery; that requires a separate fault-injection layer.
- The test-only trigger demonstrates rollback when a late SQL statement errors; the tests do not yet cover unpredictable server crashes or power loss.
- Race safety for **simultaneous** same-key requests and multiple wallet transfers has NOT been verified. This is explicitly deferred to Increment 4.
- The current schema prevents duplicate ledger types and ensures positive amounts, but does not declaratively enforce *exactly* two matching ledger postings for every processed transfer or all historical-row immutability. The service and integration assertions implement that contract for the supported write path. A privileged manual SQL writer could bypass some invariants.
- Input validation errors and new missing-wallet requests are not durably recorded and are outside the terminal-result replay contract; this assumption must be documented in the final PR.
- The test database must be **disposable**, never a production database. Each subtest creates and drops only its randomly named schema.

## Run on Windows PowerShell

```powershell
cd 'C:\path\to\robustrade-wallet-transfer-increment-3'
psql --version
go version
go mod tidy

# Use your actual credentials; this must be a disposable DB.
$env:WALLET_TEST_DATABASE_URL = 'postgres://postgres:YOUR_PASSWORD@localhost:5432/wallet_transfer_test?sslmode=disable'
psql $env:WALLET_TEST_DATABASE_URL -X -v ON_ERROR_STOP=1 -c 'SELECT current_database();'

# First run Increment 3 only (5 cases, with nested changed-payload variants).
go test ./internal/service -run '^TestPostgresIdempotencyAndRollback$' -count=1 -v

# Then rerun ALL historical and current tests without the Go test cache.
go test ./... -count=1 -v
```

### Strict all-tests-no-skips check

Run after `go mod tidy` and a working database connection:

```powershell
$lines = @(& go test ./... -count=1 -v 2>&1)
$exitCode = $LASTEXITCODE
$log = $lines -join "`n"
$log
if ($exitCode -ne 0) { throw 'Go tests FAILED' }
if ($log -match '--- SKIP:') { throw 'Tests were SKIPPED; verification incomplete' }
$required = @(
    'TestPostgresMigrationContract',
    'TestPostgresTransferService/successful_transfer',
    'TestPostgresTransferService/insufficient_funds_commits_failure_without_money_movement',
    'TestPostgresIdempotencyAndRollback/repeated_successful_transfer_returns_stored_result',
    'TestPostgresIdempotencyAndRollback/different_payload_conflicts_before_validating_nonexistent_wallet',
    'TestPostgresIdempotencyAndRollback/failed_transfer_stays_failed_after_funding_via_another_transfer',
    'TestPostgresIdempotencyAndRollback/database_error_after_financial_writes_rolls_back_claim_balances_and_ledger',
    'TestPostgresIdempotencyAndRollback/lost_response_after_commit_can_be_reconstructed_by_fresh_service'
)
foreach ($name in $required) {
    if (-not $log.Contains("--- PASS: $name (")) {
        throw "Missing explicit PASS: $name"
    }
}
Write-Host 'Required PostgreSQL tests PASSED; no failures/skips.' -ForegroundColor Green
```

Note: The nested payload variants are also verified as PASS by the parent subtest; inspect their individual output, too. On a failure, share its full error (prefer the relevant lines of `-v` output) rather than changing database migrations blindly.

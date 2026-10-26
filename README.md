# Wallet Transfer Service

A focused Go and PostgreSQL service implementing Robustrade's wallet-to-wallet transfer assignment. `POST /transfers` atomically moves integer minor currency units between existing wallets, writes a double-entry ledger, and persists idempotent outcomes that survive retries and restarts.

## Requirements and architecture

- **Go 1.23+**, **PostgreSQL**, and `psql` on `PATH` for applying migrations and running integration tests.
- Standard-library HTTP server and `database/sql`, with `github.com/lib/pq` as the PostgreSQL driver. There is no ORM, queue, worker, or external payment provider.
- `internal/handler`: JSON and HTTP concerns only. `internal/service`: transfer workflow and retry policy. `internal/repository`: PostgreSQL transactions and SQL. `internal/domain`: request/result types and validation.
- Three tables: `wallets`, `transfers`, `ledger_entries`. See [the design note](docs/DESIGN.md) for schema, transaction boundaries, invariants, assumptions, and residual risks.

## Database setup (Windows PowerShell)

Use **different, non-production databases** for the runnable service and tests. The integration suite creates/drops isolated schemas in the test database; the test account must have `CREATE` privileges.

```powershell
# From the project root; PostgreSQL is assumed to be running locally.
# Example uses the postgres superuser; a dedicated restricted user is preferable outside local development.
$env:PGPASSWORD = 'YOUR_PASSWORD'  # Local shell only; don't commit credentials.
psql -h localhost -U postgres -d postgres -X -v ON_ERROR_STOP=1 -c 'CREATE DATABASE wallet_transfer_dev;'
psql -h localhost -U postgres -d postgres -X -v ON_ERROR_STOP=1 -c 'CREATE DATABASE wallet_transfer_test;'

$env:DATABASE_URL = 'postgres://postgres@localhost:5432/wallet_transfer_dev?sslmode=disable'
$env:WALLET_TEST_DATABASE_URL = 'postgres://postgres@localhost:5432/wallet_transfer_test?sslmode=disable'
```

Create each database **only if it does not already exist**. Do not run destructive setup commands against an existing database you need to keep.

Apply the schema once to your development database:

```powershell
psql $env:DATABASE_URL -X -v ON_ERROR_STOP=1 -f '.\migrations\000001_init.up.sql'
```

The migration contains a transaction, creates constraints and append-only ledger/opening-balance triggers, and is **not idempotent**; don't reapply it to an already migrated database. `000001_init.down.sql` reverses the schema and **destroys the data**; use only on disposable databases. The server checks for the tables but does not run migrations automatically.

### Seed sample wallets

Wallet creation and external funding are intentionally outside the assignment. Seed wallets with immutable opening balances equal to their initial current balances. Later changes to balances must occur through transfers, not manual SQL updates.

```powershell
psql $env:DATABASE_URL -X -v ON_ERROR_STOP=1 -c "INSERT INTO wallets (id, opening_balance, balance) VALUES ('demo-A',50000,50000), ('demo-B',10000,10000) ON CONFLICT (id) DO NOTHING;"
```

Amounts are integer minor units of one unspecified currency (`100` means 100 minor units). There is no floating-point monetary arithmetic.

## Run the HTTP server

```powershell
go mod download
go mod verify
$env:DATABASE_URL = 'postgres://postgres@localhost:5432/wallet_transfer_dev?sslmode=disable'
$env:HTTP_ADDR = '127.0.0.1:8080'  # Optional; default is :8080.
go run ./cmd/walletd
```

The server pings PostgreSQL, refuses to start if the three tables are missing, and handles SIGINT/SIGTERM with graceful HTTP shutdown. Set `DATABASE_URL` and arrange migrations before startup. `HTTP_ADDR` controls the listening address. The default binds all interfaces; use `127.0.0.1:8080` for local evaluation. This is a demo API **without authentication or TLS**, not a public-facing payment gateway.

### API: `POST /transfers`

```http
POST /transfers
Content-Type: application/json

{"idempotencyKey":"demo-001","fromWalletId":"demo-A","toWalletId":"demo-B","amount":100}
```

PowerShell example:

```powershell
$body = @{ idempotencyKey='demo-001'; fromWalletId='demo-A'; toWalletId='demo-B'; amount=100 } | ConvertTo-Json -Compress
Invoke-RestMethod -Uri 'http://127.0.0.1:8080/transfers' -Method Post -ContentType 'application/json' -Body $body
```

**Responses:**

| Situation | HTTP | JSON example / note |
|---|---|---|
| Committed transfer or identical successful replay | `201` | `{"transferId":"<uuid>","status":"PROCESSED"}` |
| Insufficient funds or identical replay of that failure | `409` | `{"transferId":"<uuid>","status":"FAILED","failureReason":"INSUFFICIENT_FUNDS"}` |
| Same key, different source, destination, or amount | `409` | `{"error":"idempotency_conflict","message":"..."}` |
| New key referencing a nonexistent wallet | `404` | `wallet_not_found` |
| Malformed/invalid JSON or invalid request structure | `400` | `invalid_json` or `invalid_request` |
| Wrong content type / body over 8 KiB | `415` / `413` | `unsupported_media_type` / `request_too_large` |
| Exhausted aborted-transaction retries | `503` | `outcome_unknown` |
| Unexpected database/server error | `500` | Sanitized `outcome_unknown` |

The first successful response and all identical replays return the **same HTTP status and JSON body**, including after a service restart. The response intentionally omits mutable balances and timestamps. The idempotency key is **globally unique** for this service and retained indefinitely. An existing key wins over wallet-existence checks for a structurally valid conflicting request. A new key with a nonexistent wallet or an invalid request is rejected without a durable transfer record.

On an `outcome_unknown` error, **retry the same request with the same key** after connectivity recovers. A network error at commit time cannot prove whether the transfer committed. Do not generate a new idempotency key just to retry it.

## Tests

Use a disposable test database and set `WALLET_TEST_DATABASE_URL`. Some migration tests skip when PostgreSQL or `psql` is absent, so **a package-level `ok` is not sufficient evidence of complete verification**.

```powershell
$env:WALLET_TEST_DATABASE_URL = 'postgres://postgres@localhost:5432/wallet_transfer_test?sslmode=disable'
$env:PGPASSWORD = 'YOUR_PASSWORD'
go mod download
go mod verify

# All suites, fresh execution with individual test output.
go test ./... -count=1 -v -timeout 8m

# Strict wrapper: fails on any skip, failure, or missing mandatory PostgreSQL suite.
.\scripts\verify.ps1

# Optional: repeat the concurrency suite to look for scheduling-dependent issues.
go test ./internal/service -run '^TestPostgresConcurrentTransfers$' -count=3 -v -timeout 8m

# Separate static and race checks.
go vet ./...
go test -race ./... -count=1 -timeout 12m
```

`go test -race` needs a working Go race-enabled toolchain; on Windows this may require a supported C compiler. It was **not part of the reported Increment 5 full-suite verification** unless run separately. PostgreSQL integration tests use randomized schemas and clean them up; they exercise the real migration with `psql`, not fake repositories. See [test plan and verification status](docs/TEST_PLAN.md).

**Verification status (2026-10-26):** The candidate reported that all tests through Increment 5—including real PostgreSQL HTTP integration, migrations, rollback, retry and concurrency—passed locally with **no skips**; the concurrency suite passed in three additional runs in Increment 4. Small final-review changes and the new overflow regression test have **not yet been verified against PostgreSQL**. See the final verification checklist below and rerun it before checking PR test boxes.

## Accounting and failure assumptions

- A successful transfer moves exactly one debit and one credit of the same amount, changes both wallet balances, and commits a terminal `PROCESSED` record together in one PostgreSQL transaction.
- Insufficient funds commits a terminal `FAILED` record with no balance changes or financial ledger entries. It is replayed as FAILED even if the source balance later increases; a new business attempt needs a new key.
- `PENDING` exists only within the synchronous transaction; only terminal outcomes are committed. Crashes before commit roll back the claim and financial operations.
- Each wallet has an immutable, **out-of-ledger opening balance** established during provisioning. Reconciliation is `current balance = opening balance + credits - debits`. This simplifies the assignment but is not a complete funding ledger.
- Transactions lock both wallet rows in increasing ID order with `FOR NO KEY UPDATE`, and use a conditional debit. Database-enforced uniqueness resolves concurrent duplicate keys. Only confirmed `40P01`/`40001` aborts trigger bounded whole-transaction retries.
- Constraints do not independently enforce all cross-table accounting invariants. Financial correctness still relies on the controlled write path, integration tests, and reconciliation checks. Credit overflow is expected to abort atomically; the newly added regression requires local PostgreSQL verification.
- No authentication, external funding, multiple currencies, refunds/reversals, payment-provider effects, or durable asynchronous pending jobs are implemented.

## Project contents

```text
cmd/walletd/                     HTTP server entry point
internal/domain/                request validation and result types
internal/handler/               HTTP request/response adapter
internal/service/               transactional workflow and integration tests
internal/repository/            PostgreSQL operations and error classification
migrations/                     initial SQL schema and migration tests
scripts/verify.ps1              fail-on-skip PostgreSQL verification
scripts/check-format.ps1        Go formatting check
docs/DESIGN.md                   final schema, invariants, and failure semantics
docs/TEST_PLAN.md               verified vs unverified coverage
docs/REVIEW.md                  final review findings and risk boundaries
docs/AI_USAGE.md                disclosure text and transcript obligation
docs/history/                   original incremental implementation notes
submission/PR_DESCRIPTION.md    PR draft matching Robustrade's template
submission/SUBMISSION_CHECKLIST.md    final verification and Git steps
```

## Submission

Fork [Robustrade/wallet-transfer-assignment](https://github.com/Robustrade/wallet-transfer-assignment), create a branch `solution/<your-name>`, copy the application files into your fork **without deleting the repository's original assignment/template files**, and open a PR targeting the upstream `main` branch. Follow [the PR draft](submission/PR_DESCRIPTION.md) and [the AI-use disclosure](docs/AI_USAGE.md). **Include a full AI session transcript (in the repo or emailed to Robustrade); if that's impossible, all prompts used.** The transcript isn't bundled here because an authentic export must come from your account.

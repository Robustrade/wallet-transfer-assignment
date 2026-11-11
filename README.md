# Wallet Transfer Assignment Repository

This repository is a reusable coding assignment template for evaluating backend engineers on wallet transfers, idempotency, concurrency control, and double-entry ledger design.

## Included

- `ASSIGNMENT.md` - candidate-facing prompt
- `.github/pull_request_template.md` - required PR structure
- `.github/workflows/ci.yml` - lint, format, test placeholder workflow
- `.github/workflows/sonarqube.yml` - SonarQube pull request analysis
- `.github/copilot-instructions.md` - repository-level Copilot review guidance
- `evaluation_guide.md` - reviewer rubric
- `branch-protection-checklist.md` - GitHub setup checklist

## Intended use

1. Mark this repository as a GitHub template repository.
2. Create one private repository per candidate from the template.
3. Add the candidate as a collaborator.
4. Ask them to submit via a pull request into `main`.
5. Enable required checks, SonarQube, and Copilot review in GitHub.

## Notes

- Copilot automatic pull request review is configured in GitHub repository or organization settings, not purely through files in the repo.
- The `copilot-instructions.md` file included here provides repository-specific review guidance once Copilot review is enabled.
- The CI workflow is language-agnostic by default and expects you to set the `LINT_CMD`, `FORMAT_CHECK_CMD`, and `TEST_CMD` repository variables or replace the commands directly.

## How to Submit Assignment

1. **Fork this repository** to your own GitHub account.
2. Complete the assignment described in [`ASSIGNMENT.md`](./ASSIGNMENT.md).
3. **Raise a Pull Request** back to this repository (`main` branch) with your full solution.

Your PR branch should be named: `solution/<your-name>` (e.g., `solution/jane-doe`).

## Solution — Wallet Transfer Service (Go)

**Status:** the required transfer workflow is implemented and verified on Go 1.24/PostgreSQL 16, including real-executable startup, replay after restart, and signal-driven shutdown. See [VERIFICATION.md](./VERIFICATION.md) for results and the remaining remote-CI gaps. Stack: `net/http`, `log/slog`, GORM, and its PostgreSQL driver (pgx).

### Run locally (Windows PowerShell)

Follow these steps in order from the repository root. All local command examples use PowerShell. Database creation and migrations are explicit setup steps; the application does not perform them on startup.

1. **Install and check prerequisites.** Install Git, Go 1.24 or a compatible newer version, and PostgreSQL 16 with its command-line tools. Start PostgreSQL using Windows Services if it is stopped. Remember the database username, password, and port selected during installation. For lint/format checks, also install golangci-lint v2. For race tests, install MSYS2 UCRT64 GCC and add its directory as shown in step 7; running the server does not require GCC.

   ```powershell
   # Open PowerShell in your local checkout, then check the tools.
   go version
   # Adjust 16 to your installed PostgreSQL version if needed.
   $env:PATH = "C:\Program Files\PostgreSQL\16\bin;$env:PATH"
   psql --version
   Get-Service *postgres*
   pg_isready -h 127.0.0.1 -p 5432
   go mod download
   ```

   Use the same PostgreSQL port in the checks and your connection URLs. Continue after PostgreSQL reports that it is accepting connections.

2. **Create and edit `.env`.** Copy [`.env.example`](./.env.example) without overwriting an existing local configuration, then replace `USER` and `PASSWORD` in both URLs with your PostgreSQL credentials. Keep the application database `wallet` separate from the integration database `wallet_test`. The test database name must end in `_test`.

   ```powershell
   if (-not (Test-Path .env)) { Copy-Item .env.example .env }
   notepad .env
   ```

   URL-encode special characters inside URL usernames/passwords: `@` becomes `%40`, `#` becomes `%23`, and `%` becomes `%25`. For example, password `hello@123` is written as `hello%40123` in the URL. Keep the URL values quoted as in the example. `sslmode=disable` is for local development only. `.env` is ignored by Git; never commit real credentials.

3. **Load `.env` into this PowerShell session.** Saving the file alone does not configure the process. This block supports the example's plain or quoted `KEY=value` entries. Repeat it after editing `.env` or opening a new terminal.

   ```powershell
   Get-Content .env | ForEach-Object {
       if ($_ -match '^\s*([A-Za-z_][A-Za-z0-9_]*)=(.*)$') {
           $name = $Matches[1]
           $value = $Matches[2].Trim()
           if ($value.Length -ge 2 -and (
               ($value.StartsWith("'") -and $value.EndsWith("'")) -or
               ($value.StartsWith('"') -and $value.EndsWith('"'))
           )) { $value = $value.Substring(1, $value.Length - 2) }
           [Environment]::SetEnvironmentVariable($name, $value, 'Process')
       }
   }
   if (-not $env:DATABASE_URL -or -not $env:TEST_DATABASE_URL) {
       throw 'Set DATABASE_URL and TEST_DATABASE_URL in .env and load it again.'
   }
   ```

4. **Create databases, apply migrations, and seed demo wallets.** Run the setup script below after loading `.env`. It creates missing application and test databases, applies the single initial schema migration, and seeds the demo wallets. Existing databases are preserved, schema version 2 is skipped, and seed data does not reset existing balances. Schema version 2 is retained for compatibility with databases created before the migrations were consolidated. The test database stays separate; tests manage their own schemas.

   ```powershell
   $appUri = [Uri]$env:DATABASE_URL
   $testUri = [Uri]$env:TEST_DATABASE_URL
   $appDatabase = [Uri]::UnescapeDataString($appUri.AbsolutePath.TrimStart('/'))
   $testDatabase = [Uri]::UnescapeDataString($testUri.AbsolutePath.TrimStart('/'))
   if (-not $appDatabase -or -not $testDatabase.EndsWith('_test') -or $env:DATABASE_URL -eq $env:TEST_DATABASE_URL) {
       throw 'Configure separate application and *_test database URLs before setup.'
   }
   $adminUrl = $env:DATABASE_URL -replace '^(postgres(?:ql)?://[^/]+/)[^?]*(\?.*)?$', '${1}postgres${2}'
   $testAdminUrl = $env:TEST_DATABASE_URL -replace '^(postgres(?:ql)?://[^/]+/)[^?]*(\?.*)?$', '${1}postgres${2}'
   psql -X -v ON_ERROR_STOP=1 -d $adminUrl -v "application_database=$appDatabase" -v "test_database=$testDatabase" -v "test_admin_url=$testAdminUrl" -f migrations/setup.sql
   if ($LASTEXITCODE -ne 0) { throw 'Database setup failed; resolve the PostgreSQL error before starting the server.' }
   ```

   The role in each URL must have permission to create its database; otherwise an administrator must create it and grant access first. The setup script connects to each server's existing `postgres` database before creating the target database. It is a `psql` script using commands such as `\connect` and `\ir`; run it with `psql`, not through GORM or a generic SQL executor. Database creation runs outside transactions, while the initial schema migration retains its transaction and advisory lock.

5. **Confirm setup succeeded.** The setup output should show the test database connection and application schema `version = 2`, `dirty = f`. You can verify both connections separately:

   ```powershell
   psql -X -v ON_ERROR_STOP=1 -d $env:DATABASE_URL -c 'SELECT version, dirty FROM schema_migrations;'
   psql -X -v ON_ERROR_STOP=1 -d $env:TEST_DATABASE_URL -c 'SELECT current_database();'
   ```

   If either fails, fix it before continuing. Passing tests against `wallet_test` does not by itself configure the application database. Later launches only need environment loading and server startup; rerun step 4 when setting up a new database or applying these migrations.

6. **Start the server.** Run in the same terminal where `.env` was loaded.

   ```powershell
   go run ./cmd
   ```

   Expect a `server listening` message at `127.0.0.1:8080` with the example configuration. Keep this terminal open and use a second PowerShell terminal for the request in **Try it** below. Press Ctrl+C to stop the server. For later launches, repeat step 3 and this step; the setup script creates databases and applies missing migrations when needed.

7. **Run tests with the race detector.** In a terminal with `.env` loaded, configure the installed MSYS2 UCRT64 compiler. Adjust the path if MSYS2 is installed elsewhere.

   ```powershell
   $env:PATH = "C:\msys64\ucrt64\bin;$env:PATH"
   $env:CGO_ENABLED = '1'
   gcc --version
   go test -race -count=1 ./...
   ```

   All packages should pass with no `WARNING: DATA RACE` reports. The test role needs permission to create/drop schemas in `wallet_test`; tests clean up their own schemas and never reset application data. Tests skip database coverage when `TEST_DATABASE_URL` is unset or `-short` is used. See **Test, lint, format** for additional checks.

### Local setup troubleshooting

To see the specific PostgreSQL error behind `database connection failed`, run this in the terminal where `.env` is loaded:

```powershell
psql -X -d $env:DATABASE_URL -c 'SELECT 1;'
```

| Error or symptom | Action |
| --- | --- |
| `DATABASE_URL is required` | Complete steps 2–3 in the same terminal used to start the server. |
| `psql` is not recognized | Set the PostgreSQL client path in step 1 to your installation directory. |
| Connection refused / server not accepting connections | Start the PostgreSQL Windows service; verify the host and port in `.env`. |
| Could not translate host name | Check URL syntax and URL-encode special characters inside credentials (step 2). |
| Password authentication failed | Correct the database username/password in `.env`, then repeat step 3. |
| Database `wallet` or `wallet_test` does not exist | Create the missing database in step 4. Application and test databases are separate. |
| Permission denied to create database | Have a PostgreSQL administrator create the database and grant your role access. |
| Schema unavailable or incompatible | Check the schema state and rerun setup in step 4. |
| `-race requires cgo` / GCC not found | Complete step 7 with an installed UCRT64 GCC and CGO enabled. |
| Service tests report `test database unavailable` | Run the connection check with `$env:TEST_DATABASE_URL`; fix that database's credentials, existence, or permissions. |
| Listener address already in use | Stop the existing server or change `HTTP_PORT` in `.env`, reload it, and restart. |

| Variable | Default | Purpose |
| --- | --- | --- |
| `DATABASE_URL` | Required | Application PostgreSQL DSN |
| `HTTP_HOST` | `127.0.0.1` | Listener interface |
| `HTTP_PORT` | `8080` | Listener port, 1-65535 |
| `DB_OPERATION_TIMEOUT` | `5s` | Startup check deadline and overall transfer lookup/reservation/processing budget |
| `HTTP_SHUTDOWN_TIMEOUT` | `15s` | Active-request drain budget |
| `MAX_TRANSFER_AMOUNT` | `1000000000000` | Positive int64 business limit in minor units |
| `TEST_DATABASE_URL` | None | Required to execute processing integration tests; database name must end in `_test` |

Startup rejects missing configuration, unreachable databases, and missing/dirty/incompatible schema versions. SIGINT/SIGTERM stops new connections and gives active requests the configured grace period using a fresh shutdown context, then closes the database pool. If the deadline expires, remaining connections are closed and the process exits with a shutdown error. There is no application panic recovery, background recovery, or automatic restart. Only `POST /transfers` is registered.

The application does not load `.env` automatically. Configuration comes from process environment variables.

Configuration and the GORM database are process-wide singletons. `config.Get()` uses `sync.OnceValues` to return validated configuration copies and any initialization error; changing environment configuration requires restarting the process. `postgres.Init()` serializes initialization with a mutex and caches only a successful database handle. Failed calls return their errors, and later explicit calls may retry; no background retry or recovery loop is started. Startup still exits on its first failure and owns database shutdown; the closed singleton is not reopened.

### Try it

After completing database setup and starting the server:

```powershell
$request = @{
    Uri = 'http://localhost:8080/transfers'
    Method = 'Post'
    ContentType = 'application/json'
    Body = (@{
        idempotencyKey = 'abc123'
        fromWalletId = 'wallet_1'
        toWalletId = 'wallet_2'
        amount = 100
    } | ConvertTo-Json)
}
Invoke-WebRequest @request
```

Repeat the same request to replay its saved terminal status and body without moving money again. Reusing `abc123` with a different source, destination, or amount returns `409`.

The contract requires nonblank wallet IDs and idempotency keys of at most 128 Unicode characters, without character whitelists. UUID keys are accepted; values remain case-sensitive and unchanged. The client supplies existing source/destination wallet IDs and its idempotency key; PostgreSQL generates new transfer UUIDs and ledger IDs. Amounts must be positive integer literals within `MAX_TRANSFER_AMOUNT`. Malformed/oversized JSON (over 4096 bytes), unknown fields, extra JSON values, same-wallet requests, and blank/overlength identifiers return `400` before service invocation.

Only `POST /transfers` is in the required API scope. Wallet creation, balance, and ledger endpoints are excluded from this solution; wallets are initialized through seed data.

### Architecture and transfer flow

```text
HTTP handler -> Transfer service -> PostgreSQL repository -> PostgreSQL
                       |
                  Domain models
```

Handlers decode and validate requests and map errors to HTTP responses. The service owns business rules, idempotency, and transaction boundaries. Repositories execute SQL through explicit transactions; domain models define entities and allowed state transitions.

The data model contains `wallets`, `transfers`, `ledger_entries`, and `idempotency_records`. Amounts are positive integer minor units in one currency. Stored wallet balances support fast checks; the append-only ledger records the audit trail.

1. Validate the request and hash the canonical source, destination, and amount. An existing key must match this payload before any result is replayed.
2. In a short reservation transaction, create a `PENDING` transfer and a unique idempotency record. A key conflict rolls back the tentative transfer and resolves the winning record.
3. In a processing transaction, lock the transfer row. Replay terminal results; otherwise lock both wallet rows individually in ascending ID order using `FOR NO KEY UPDATE` under `READ COMMITTED` isolation.
4. Recheck funds while holding the locks. Success atomically updates both balances, writes one debit and one credit, marks `PROCESSED`, and saves the terminal response. Insufficient funds saves `FAILED` and its response without changing balances or ledger entries.
5. Return a terminal outcome after confirmed commit. A retry resumes a durable `PENDING` transfer. If commit acknowledgement or the HTTP response is lost, the same key resolves the persisted outcome without another financial effect.

`CreateTransfer` validates and resolves the key before invoking `ProcessTransfer`. The PostgreSQL unique key decides simultaneous reservations; a losing transaction rolls back its tentative transfer before reading the winning record. An existing key's payload is checked before wallet existence, so conflicting reuse returns `409` even while pending. Processing owns a GORM transaction at `READ COMMITTED` and saves the terminal response with financial writes. Destination overflow rolls back and leaves the reservation pending. Lock waits are capped at 2 seconds and statements at 4 seconds, further bounded by the operation timeout. No automatic retry loop is used. Terminal retries replay saved bytes; commit errors return no success result. Confirmed pending work after a lock error returns uncached `202` with `Retry-After: 1`; expired deadlines or uncertain outcomes return retryable errors.

Database constraints protect nonnegative balances, valid relationships, positive amounts, and unique ledger types per transfer. Deferred ledger validation and immutability guards protect matching ledger pairs and terminal states. Tests reconcile each stored balance with its opening balance plus credits minus debits.

| Outcome | HTTP behavior |
| --- | --- |
| Successful transfer | `201`, persisted `PROCESSED` result |
| Insufficient funds | `422`, persisted `FAILED` result |
| Invalid request / missing wallet | `400` / `404`, no key reserved |
| Key reused with different payload | `409`, no financial effect |
| Terminal duplicate | Original status and body, `Idempotent-Replayed: true` |
| Unresolved concurrent processing | `202` with `Retry-After` |
| Database outage or lock timeout | Retryable `503` |
| Unexpected internal failure | Generic `500` |

Transient failures and `202` responses are not cached as terminal results. Clients retry the same key and payload with backoff. Database waits are bounded; each client retry uses the persisted reservation/result rather than allocating a second transfer for the key.

### Test, lint, format

Run the available tests and checks in PowerShell after loading `.env`:

```powershell
go mod verify
go build ./...
golangci-lint fmt --diff # formatting check; any diff needs attention
golangci-lint run
go vet ./...
New-Item -ItemType Directory -Force profiles | Out-Null
go test -race -count=1 -v -coverprofile=profiles/coverage.out ./...

golangci-lint fmt     # apply formatting when needed
go test -short ./... # unit/HTTP tests; skips database processing tests
go test -v ./internal/service # integration tests; requires TEST_DATABASE_URL
```

Use golangci-lint v2 with `.golangci.yml`. Race tests require `$env:CGO_ENABLED = '1'` and a compatible C compiler such as GCC on PATH. The Makefile remains available for environments with GNU Make, Bash, and Unix utilities; the commands above run directly from PowerShell.
Unit tests cover startup/shutdown, validation/hashing, state transitions, decoding, and HTTP errors/headers. Real-database tests cover processing, rollback, contention, key races, conflicting payloads, saved failures, new-pool recovery, and response loss after commit. Acceptance cases exercise parallel HTTP requests, reconciliation with NUMERIC sums, append-only history, rejected overdrafts/incomplete pairs, unavailable connections, and backend termination during a credit. A test-only wrapper injects acknowledgement loss after a real COMMIT in either phase and verifies safe retry; it does not claim literal network-loss testing. Router tests use the same `NewRouter` factory as startup.

Database tests skip if `TEST_DATABASE_URL` is absent or `-short` is set; a skip is not database verification. Use a dedicated test role able to create/drop schemas; the backend-termination case targets only that test role's own processing connection. The standalone schema suite remains removed.

Latest local verification on **2026-11-11** passed after consolidating the schema migration. The test cache was cleared before running the default `make` target: **61 test functions passed, zero skipped, zero failed, and no data races reported**. This includes all 42 service/database/router test functions against PostgreSQL 16. CGO was enabled with MSYS2 UCRT64 GCC 16.2.0.

| Check | Result |
| --- | --- |
| Dependency integrity and application build | Passed |
| Formatting, lint, and vet | Passed; lint reported zero issues |
| Unit, HTTP, repository, and PostgreSQL integration tests with race detection | Passed |
| Fresh database setup and repeated setup | Passed using disposable databases |
| Statement coverage (57.4% overall) | Domain 100.0%; handler 91.4%; service 81.9%; cmd 38.2%; repository 19.6% |

Coverage is saved to `profiles/coverage.out`. The configuration and application-error packages have no test files and report 0.0% package coverage. Coverage percentages do not replace behavior checks. Test fixtures use isolated schemas; setup verification uses disposable databases and does not alter application wallet data. [VERIFICATION.md](./VERIFICATION.md) records commands, profiling results, and limitations; [AI_USAGE.md](./AI_USAGE.md) records AI disclosure and available prompts. Remote CI and SonarQube were not run as part of this local verification.

CI repository variables can use:

```text
LINT_CMD=make lint
FORMAT_CHECK_CMD=make fmt-check
TEST_CMD=make test
CGO_ENABLED=1
```

The existing CI workflow selects Go 1.24 and requires its configured self-hosted Linux runner. It installs golangci-lint v1.64.8, which is incompatible with the local v2 configuration; its installer command also needs a separate PATH-update line. These CI issues remain unresolved because `ci.yml` is being kept unchanged. SonarQube requires `SONAR_TOKEN` and `SONAR_HOST_URL`.

GORM initializes persistence with its PostgreSQL driver and default connection-pool settings. The application accesses the underlying pool only for a bounded startup ping and shutdown cleanup. Explicit SQL migrations remain separate; AutoMigrate is not used.

### Tradeoffs

| Decision | Benefit | Cost or limitation |
| --- | --- | --- |
| GORM with PostgreSQL driver | Familiar ORM persistence and explicit transaction support | Additional dependencies; financial locking and transaction boundaries still need deliberate design |
| PostgreSQL as the consistency boundary | Durable transactions, uniqueness, and locks across service instances | Requires PostgreSQL for execution and realistic integration tests |
| Separate reservation and processing transactions | Durable, recoverable `PENDING` state | Additional round trip and recovery logic |
| Recovery on client retry | No background worker required | Abandoned transfers remain pending until retried |
| Stored balances plus append-only ledger | Fast balance checks and an audit trail | Both representations must be updated atomically and reconciled |
| Deterministic wallet row locks | Prevents overspending and opposite-direction deadlocks | Busy wallets serialize transfers; contention may require retries |
| Database ledger and state guards | Protects committed invariants | Trigger logic adds complexity and needs direct tests |
| Indefinite key retention and saved responses | Stable replay across restarts and lost responses | Storage grows; a failed key stays failed even after funds change |
| Integer minor units, one currency | Exact arithmetic | Requires overflow checks; no currency conversion |

The design provides at most one committed financial effect per retained key. HTTP delivery itself cannot be guaranteed exactly once. Keys are globally scoped in a trusted assignment environment, and wallets are not physically deleted.

Extra wallet/history APIs, background workers, distributed locks, metrics infrastructure, public profiling endpoints, external payment gateways, and multi-region writes are outside scope.

### Project structure

```text
cmd/                           Entry point and dependency wiring
internal/
  appErrors/                   Unified application errors and ValidationError
  config/                      Environment configuration
  domain/                      Entities, request validation, and transfer states
  handler/                     HTTP contract and error mapping
  service/                     Transfer workflow and transaction ownership
  repository/                  SQL and database access
migrations/
  000001_initial.up.sql        Complete schema, including database-generated UUIDs
  setup.sql                    Database creation, schema application, and demo seeding
```

Unit, HTTP, and PostgreSQL integration tests live beside their packages as `*_test.go` files. No middleware, extra endpoint, or background-worker implementation is implied by the original empty scaffolding directories.

HTTP tests use the public handler API from `handler_test`, with a small local fixture and named scenarios. Request validation/hashing lives in `domain/transfer_request.go`; transfer entities and state transitions live in `domain/transfer.go`.

Application error definitions live in `internal/appErrors/errors.go`. Callers use `appErrors.Err...` and `errors.Is`/`errors.As`; HTTP response mapping stays in `handler/response.go`, and processing/startup classification stays with those workflows.

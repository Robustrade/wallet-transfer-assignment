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

---

# Solution — Wallet Transfer Service

Go + PostgreSQL implementation of the assignment in [`ASSIGNMENT.md`](./ASSIGNMENT.md). See [`DESIGN.md`](./DESIGN.md) for the original design outline.

## Architecture

```
cmd/api            entrypoint: wiring, HTTP server, graceful shutdown
internal/domain    entities, validation, state machine constants — no dependencies
internal/repository/postgres  persistence (pgx), embeds and runs the schema on startup
internal/service   transaction boundaries, locking, idempotency orchestration
internal/handler   HTTP transport (chi router), request/response DTOs
migrations         raw SQL schema, embedded into the binary via go:embed
```

## Schema Design

Four tables (see [`migrations/0001_init.sql`](./migrations/0001_init.sql)):

- **`wallets`**: `id`, `balance` (`CHECK (balance >= 0)`, prevents an overdraft ever being persisted), timestamps.
- **`transfers`**: `id`, `idempotency_key` (`UNIQUE NOT NULL`), `from_wallet_id`/`to_wallet_id` (FK to `wallets`, `CHECK (from_wallet_id <> to_wallet_id)`), `amount` (`CHECK (amount > 0)`), `status` (`CHECK` restricted to `PENDING`/`PROCESSED`/`FAILED`), `failure_reason`.
- **`ledger_entries`**: `id`, `transfer_id` (FK), `wallet_id` (FK), `entry_type` (`CHECK` restricted to `DEBIT`/`CREDIT`), `amount`.
- No separate `idempotency_records` table — see **Idempotency Strategy** below for why.

Indexes on `transfers.from_wallet_id`/`to_wallet_id` and `ledger_entries.transfer_id`/`wallet_id` support the obvious lookups (a wallet's transfer history, a transfer's ledger entries).

Amounts are `BIGINT`, denominated in the smallest currency unit (e.g. cents) — avoids floating-point rounding entirely.

## Idempotency Strategy

`transfers.idempotency_key` carries a `UNIQUE NOT NULL` constraint and *is* the durable record of "has this request been seen before" — no separate `idempotency_records` table. The original [`DESIGN.md`](./DESIGN.md) sketched one, but a second table would need to stay consistent with the transfer row it points to for no real benefit; a single `INSERT ... ON CONFLICT (idempotency_key) DO NOTHING` gives the same atomicity guarantee with less code and one less thing that can drift out of sync.

Flow in [`TransferService.CreateTransfer`](./internal/service/transfer_service.go):

1. A plain `SELECT` by `idempotency_key` short-circuits the common replay case before touching any wallet locks.
2. For a genuinely new key, `INSERT ... ON CONFLICT DO NOTHING` atomically claims it — closing the race where two requests with the same brand-new key arrive at once. Whichever loses the race simply fetches and returns the winner's row.
3. The response for a replay is always the transfer's *current* row — including a `FAILED` one. Retrying a request that failed for a deterministic reason (e.g. insufficient funds) correctly returns the same failure, not a fresh attempt.

## Concurrency Strategy

Both wallets are locked with `SELECT ... FOR UPDATE` inside a single DB transaction, in **ascending wallet-id order**, and — importantly — *before* the transfer row is inserted.

Two things had to be true simultaneously to avoid both deadlocks and lost updates, and getting only one right isn't enough:

- **Fixed lock order** prevents the textbook deadlock: two transfers moving money in opposite directions between the same wallet pair, each holding one lock and waiting on the other.
- **Locking before inserting** matters because `transfers.from_wallet_id`/`to_wallet_id` are foreign keys — inserting a transfer row takes an implicit shared lock on both wallet rows as part of the FK check. If that insert happened before the explicit `FOR UPDATE`, two concurrent transactions could each hold that weak shared lock and then deadlock trying to *upgrade* to the exclusive lock the other is holding. Taking the strong lock first means the FK check is later satisfied by a lock the same transaction already holds, with no upgrade needed.

This was not theoretical — an earlier version of this code (locking after the insert) reliably hit Postgres deadlocks under concurrent load, caught by the test described below.

## How to Run

Requires a local PostgreSQL instance (v14+; developed against v18) and Go 1.24+.

```bash
# 1. Create a database + role (adjust to taste)
psql -U postgres -c "CREATE ROLE wallet WITH LOGIN PASSWORD 'wallet';"
psql -U postgres -c "CREATE DATABASE wallet OWNER wallet;"

# 2. Configure (defaults already match the above — see .env.example)
cp .env.example .env

# 3. Run — tables are created and two test wallets seeded automatically on startup
go run ./cmd/api
```

Alternatively, `docker-compose up -d` starts a matching Postgres container if you'd rather not use a local install.

Smoke test:

```bash
curl http://localhost:8080/wallets/wallet_1
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
```

## How to Test

```bash
go test ./...            # unit tests (internal/domain) + integration tests (internal/service)
go test ./... -race      # same, with the race detector (requires CGO_ENABLED=1 + a C compiler)
```

The `internal/service` tests connect to a real Postgres (`DATABASE_URL`/`TEST_DATABASE_URL` env var, defaulting to the same local instance used by `go run`) — they exercise real row locking and transaction semantics that a mock cannot faithfully reproduce. If no database is reachable, they skip rather than fail.

Coverage:
- successful transfer: correct balances, exactly one `DEBIT` + one `CREDIT` ledger entry
- idempotent replay: same key twice → same transfer, no duplicate debit, no duplicate ledger entries
- insufficient funds → `FAILED`, balances and ledger untouched
- nonexistent wallet → error, no transfer row persisted
- concurrency: many parallel transfers from the same wallet → final balance reflects every one exactly once (no lost updates)
- concurrency: parallel transfers in *both directions* between the same wallet pair → proves the fixed lock order avoids deadlock, and balances net out correctly

## Tradeoffs / Assumptions

- `amount` is a positive integer in the smallest currency unit (e.g. cents); no currency/multi-currency handling.
- Wallets are assumed pre-existing — no wallet-creation endpoint (out of scope per the assignment).
- Idempotency is keyed purely on `idempotencyKey`, not a hash of the request body — a replay with the same key but a *different* payload silently returns the original result rather than erroring. Acceptable for this assignment's scope; a production system would likely want to detect and reject that mismatch.
- A `FAILED` transfer (e.g. insufficient funds) is treated as a valid terminal state and is itself idempotent — retrying returns the same `FAILED` result rather than re-attempting.
- No authentication/authorization — out of scope per the assignment.

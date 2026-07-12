# Wallet Transfer Service

A small, reliable service for **wallet-to-wallet transfers** with idempotent request
handling, double-entry ledger recording, and safe concurrent execution.

> The template/reviewer instructions for this repository are kept further down under
> [Repository Template Notes](#repository-template-notes).

## What's implemented

A single HTTP endpoint, `POST /transfers`, that moves money between two wallets
**atomically** and with **exactly-once semantics**. Every requirement from
[`ASSIGNMENT.md`](./ASSIGNMENT.md) is covered:

| Requirement                     | How it's implemented                                                                                       |
|---------------------------------|------------------------------------------------------------------------------------------------------------|
| **Create transfer**             | `POST /transfers` accepts `{idempotencyKey, fromWalletId, toWalletId, amount}` and returns the result.     |
| **Idempotent requests**         | The `idempotencyKey` is the PK of `idempotency_records`; the original response is stored and **replayed** on any retry — duplicates never trigger a second transfer. |
| **Wallet balances**             | Stored per-wallet balance, updated **inside** the transfer transaction; `CHECK (balance >= 0)` prevents overdraft at the DB. |
| **Double-entry ledger**         | Each transfer writes exactly one `DEBIT` + one `CREDIT` row, enforced by a **unique `(transfer_id, type)`** index, so the ledger can never be unbalanced. |
| **Transfer state machine**      | `PENDING → PROCESSED` on success, `PENDING → FAILED` on insufficient funds; state is `CHECK`-constrained.  |
| **Concurrency safety**          | Whole transfer runs in one DB transaction; both wallets are locked with `SELECT ... FOR UPDATE` in a deterministic order to prevent double-spend and deadlocks. |
| **Persistence**                 | PostgreSQL via GORM; schema in [`migrations/schema.sql`](./migrations/schema.sql).                         |
| **Layered architecture**        | Thin handler → service (business logic) → repository (persistence), with dependency inversion.             |
| **Testing**                     | Unit tests (no DB) + real-Postgres concurrency integration tests proving no double-spend and a balanced ledger. |

## How a transfer executes

A single `POST /transfers` call runs the following steps inside **one** database
transaction (see [`internal/service/transfer_service.go`](./internal/service/transfer_service.go)):

1. **Validate** the request (non-empty IDs/key, positive amount, distinct wallets).
   Invalid input is rejected before any DB work.
2. **Replay check** — if the `idempotencyKey` already exists, return its stored
   response verbatim and stop (no new side effects).
3. **Lock both wallets** with `SELECT ... FOR UPDATE`, always in sorted ID order,
   so concurrent opposing transfers can't deadlock.
4. **Insert the transfer** row in `PENDING` state.
5. **Check funds under the lock.** If insufficient, mark the transfer `FAILED`,
   store the response against the key, and return `422` (still replayable).
6. **Move the money** — debit the source, credit the destination.
7. **Write the ledger** — one `DEBIT` + one `CREDIT` row for the transfer.
8. **Mark `PROCESSED`** and **store the response snapshot** against the key.
9. **Commit.** If a concurrent duplicate committed first, this transaction rolls
   back on the unique-key violation and the caller replays the winner's response.

## Architecture

Clean layered architecture with clear separation of concerns:

```
cmd/server            process entrypoint, wiring, graceful shutdown
internal/api          HTTP handlers: validation + transport mapping (thin)
internal/service      business logic: transfer workflow, idempotency, concurrency
internal/repository   persistence abstractions (interfaces + error sentinels)
  └── postgres        GORM/Postgres implementations
internal/model        domain entities, states, and DB constraints
internal/config       environment configuration
internal/logging      leveled logging
migrations/schema.sql authoritative SQL schema
```

The `repository` package defines interfaces and the concrete `postgres` package
depends on them (dependency inversion), so the service layer never imports the ORM.

## Database schema

| table                 | purpose                                                        |
|-----------------------|----------------------------------------------------------------|
| `wallets`             | stored balance per wallet, `CHECK (balance >= 0)` backstop      |
| `transfers`           | transfer workflow record with a `PENDING/PROCESSED/FAILED` state |
| `ledger_entries`      | immutable double-entry rows (one DEBIT + one CREDIT per transfer) |
| `idempotency_records` | maps an idempotency key to its transfer **and stored response** |

Integrity is enforced at the database, not just in application code:

- `wallets.balance CHECK (balance >= 0)` — a hard backstop against double spend.
- `transfers.state CHECK (... IN ('PENDING','PROCESSED','FAILED'))` and
  `CHECK (from_wallet_id <> to_wallet_id)`.
- `ledger_entries` **unique `(transfer_id, type)`** — guarantees exactly one debit
  and one credit per transfer, so the ledger can never be unbalanced.
- Foreign keys tie ledger entries and idempotency records back to real transfers/wallets.
- Indexes on `ledger_entries(wallet_id)`, `transfers(from_wallet_id)`,
  `transfers(to_wallet_id)`, and `transfers(state)`.

Balances are **stored and updated inside the transfer transaction** (not derived on
read) for O(1) balance reads; the ledger remains the immutable source of truth for audit.

## Idempotency strategy (exactly-once at the API level)

- Each request carries an `idempotencyKey` (primary key of `idempotency_records`).
- On success **and** on terminal failure, we store a **snapshot of the response**
  (JSON) alongside the key, inside the same transaction as the balance changes.
- A duplicate request returns the **exact original result** — including the balances
  captured at execution time — even if the wallets have since changed. This answers
  "what if the first request committed but the response was lost?": the retry replays
  the stored result rather than re-executing.
- Concurrent duplicates race on the unique key. The loser's transaction rolls back
  (no orphan transfer, no double side effects) and then **replays the winner's stored
  response** instead of returning an error.

## Concurrency handling

- The whole transfer runs in a single database transaction.
- Both wallets are locked with `SELECT ... FOR UPDATE` in a **deterministic (sorted)
  order** to prevent deadlocks between opposing transfers (`A→B` and `B→A`).
- Insufficient funds is checked under the lock and recorded as a terminal `FAILED`
  transfer, so retries of the same key return the same outcome.
- Even if application logic had a bug, the `CHECK (balance >= 0)` constraint would
  reject a double spend at the database.

### Failure / retry semantics

| scenario                              | behavior                                             |
|---------------------------------------|------------------------------------------------------|
| duplicate request (same key)          | returns original stored response, no new side effects |
| concurrent duplicate                  | one commits, others replay the same stored response   |
| insufficient funds                    | `FAILED` transfer recorded; `422` returned; replayable |
| unknown wallet                        | `404`, no transfer created                            |
| self-transfer / invalid amount        | `400`, no transfer created                            |

## API

The service exposes a single, focused endpoint. Everything else (balances,
history, health) is intentionally out of scope — the goal is a correct,
concurrency-safe transactional core, not feature breadth.

```
POST /transfers        create a transfer (idempotent)
```

Create request:

```json
{ "idempotencyKey": "abc123", "fromWalletId": "wallet_1", "toWalletId": "wallet_2", "amount": 100 }
```

Create response (`201`):

```json
{
  "transferId": "…", "fromWalletId": "wallet_1", "toWalletId": "wallet_2",
  "amount": 100, "state": "PROCESSED", "fromWalletBalance": 400,
  "toWalletBalance": 200, "createdAt": "…"
}
```

## Running locally

```bash
export DATABASE_URL="postgres://user:pass@localhost:5432/wallet?sslmode=disable"
# optional: LISTEN_ADDR (:8080), LOG_LEVEL (INFO), DB_MAX_OPEN_CONNS, DB_MAX_IDLE_CONNS
go run ./cmd/server
```

The service does not create its own schema. `migrations/schema.sql` is the
authoritative DDL and is applied when the database is provisioned (locally, the
Postgres container runs it via `/docker-entrypoint-initdb.d/` on first init). The
service assumes the schema already exists.

## Running against real Postgres

`testenv/` spins up a real Postgres (schema applied automatically via the image's
init scripts), seeds two wallets, and starts the service on `:8080`.

```bash
cd testenv && ./run-local.sh
```

Then, from another shell, hit the endpoint:

```bash
curl -s http://localhost:8080/transfers \
  -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"demo-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
```

Inspect the resulting state (balances never go negative; the ledger balances):

```bash
docker compose -f testenv/docker-compose.yml exec -T postgres \
  psql -U wallet -d wallet -c "SELECT wallet_id, balance FROM wallets ORDER BY wallet_id;"
```

Press Ctrl+C in the `run-local.sh` shell to stop the service and tear down Postgres.

### Manual steps (without the script)

```bash
# 1. start Postgres — schema.sql is applied by the container's init scripts
docker compose -f testenv/docker-compose.yml up -d

# 2. seed two wallets
docker compose -f testenv/docker-compose.yml exec -T postgres \
  psql -U wallet -d wallet < testenv/seed.sql

# 3. run the service
DATABASE_URL="postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable" \
  go run ./cmd/server
```

## Testing

```bash
go test ./...
```

Unit tests use in-memory mocks and exercise the real `CreateTransfer` logic through
an injectable transaction runner (no database required). They cover: successful
transfer + ledger balancing, duplicate-key replay, **snapshot semantics** (replay
returns the original balances), concurrent-duplicate conflict resolution, insufficient
funds (with replay), self-transfer rejection, and unknown wallet.

### Concurrency integration tests (real Postgres)

`test/integration` runs the **real** repositories and service against a live
Postgres and fires many transfers concurrently to prove the two invariants that
matter for a money system: **no double spend** and a **balanced ledger**
(ΣDEBIT = ΣCREDIT). It is gated on `TEST_DATABASE_URL`, so `go test ./...` still
passes with no database available. To run it:

```bash
# start a throwaway Postgres (or reuse testenv/docker-compose.yml)
docker compose -f testenv/docker-compose.yml up -d

TEST_DATABASE_URL="postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable" \
    go test ./test/integration/... -v
```

- `TestConcurrentDistinctTransfersNoDoubleSpend` — 50 concurrent distinct-key
  transfers against a wallet that can only fund 10 of them; asserts exactly 10
  succeed, the balance never goes negative, and the ledger balances.
- `TestConcurrentDuplicatesSingleSideEffect` — 30 concurrent requests sharing one
  idempotency key; asserts the money moves exactly once and every caller observes
  the same transfer.

## Assumptions & tradeoffs

- Amounts are integer minor units (`int64`); no currency/multi-asset handling.
- Balance is stored (not ledger-derived) for fast reads; the ledger stays authoritative.
- A terminal `FAILED` outcome is recorded against the idempotency key, so a given key
  always yields one deterministic result (a client must use a new key to retry later).
- The schema is applied at database provisioning time (via the Postgres image's
  init scripts locally); the application never issues DDL. In production the same
  `migrations/schema.sql` is applied by your DB provisioning/migration step.

---

# Repository Template Notes

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

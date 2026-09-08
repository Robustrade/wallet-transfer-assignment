<!--
Ready-to-paste PR body for the solution branch. Fill in the AI-disclosure
transcript link before opening the PR.
-->

## Summary

Wallet-to-wallet transfer service with API-level exactly-once semantics, a
double-entry ledger, and concurrency-safe balances, in a four-layer architecture
(handler → service → repository → domain). PostgreSQL is the persistence backend;
migrations are embedded and applied on startup. `./bin/app_startup.sh` builds,
starts, seeds, and smoke-tests every endpoint.

## AI disclosure

1. **Tool:** Claude Code (Anthropic), Sonnet.
2. **How I use it:** as a pair-programmer under close direction — I specify the
   architecture, constraints, and acceptance criteria; it scaffolds packages,
   drafts implementations and tests, and iterates on my review. I run the build,
   tests, linter, and the app myself and push back on anything I would not ship.
   For this task it was used to lay out the four layers, write the Postgres
   repository + transactional workflow, the test suites, and the tooling
   (`Makefile`, `app_startup.sh`, embedded migrator).
3. **Transcript:** attached as `docs/ai-session-transcript.md` / emailed with the
   submission. Key prompts: define a 4-layer Go structure with Postgres +
   interfaces + TDD; scaffold it as working code compatible with the CI/Sonar
   config; make idempotency and concurrency correct under retries; verify with a
   real Postgres; keep PostgreSQL as the only real adapter (in-memory store is a
   test fake); documentation-first design note.

## Schema Design

Four tables ([`internal/repository/migrations/0001_init.up.sql`](../internal/repository/migrations/0001_init.up.sql)):

| Table | Notable constraints |
| --- | --- |
| `wallets` | `balance BIGINT CHECK (balance >= 0)`, `version` for optimistic checks, `currency` |
| `transfers` | `state CHECK (PENDING/PROCESSED/FAILED)`, `CHECK (from_wallet_id <> to_wallet_id)`, **`UNIQUE (idempotency_key)`**, FKs to `wallets` |
| `idempotency_records` | PK on `key`, `request_hash`, FK to `transfers` |
| `ledger_entries` | `type CHECK (DEBIT/CREDIT)`, `amount CHECK (> 0)`, **`UNIQUE (transfer_id, type)`**, FK to `wallets` and `transfers`, index on `wallet_id` |

Amounts are `BIGINT` minor units (no floats). Correctness is enforced in the
database, not application code: duplicate transfers are impossible
(`transfers.idempotency_key` unique), a transfer can never have more or fewer
than two ledger rows (`UNIQUE (transfer_id, type)` + equal amounts from one code
path), ledger rows cannot outlive their transfer (FK), balances cannot go
negative (CHECK).

## Idempotency Strategy

- **Durable claim:** creating the `transfers` row *is* the claim — its
  `UNIQUE (idempotency_key)` constraint is the exactly-once guarantee. A
  `request_hash` (sha256 of from/to/amount/currency) is stored in
  `idempotency_records`.
- **Flow:** fast-path lookup by key outside any transaction → replay if found.
  Otherwise, inside the transaction, insert the transfer; a unique-violation
  means another request won the race, so the transaction aborts and the stored
  result is replayed from outside it.
- **Replay vs conflict:** same key + same payload → the original transfer is
  returned (`200`), no new side effects. Same key + different payload → `409`.
- **Failure replay:** insufficient funds commits a `FAILED` transfer (no balance
  or ledger change), so a retry replays the failure instead of re-executing.
- Safe across process restarts and duplicate delivery because the guarantee is a
  database constraint, not in-memory state.

## Concurrency Strategy

- **One transaction per transfer** ([`DB.WithinTx`](../internal/repository/postgres/db.go)).
- **Pessimistic row locks:** both wallet rows are read with
  `SELECT ... FOR UPDATE` in **ascending wallet-id order** (deadlock-free between
  `A→B` and `B→A`), acquired **before** the transfer INSERT so the INSERT's
  FK `FOR KEY SHARE` locks don't force a deadlock-prone lock upgrade. This
  serialises concurrent debits of the same wallet — no read-then-write race, no
  double spend.
- **Isolation:** `READ COMMITTED` + explicit locking. Pessimistic locking chosen
  over optimistic retry because contention on a hot wallet is expected and we
  want bounded latency, not retry storms.
- **Transient errors:** deadlock / serialization failures are retried a bounded
  number of times inside `WithinTx`.
- **Proof:** `TestPostgres_ConcurrentDebits` — 50 goroutines debit one wallet
  that funds only 10; exactly 10 succeed, final balances exact, 20 ledger rows.

## How to Run

```bash
make db-up                      # local Postgres (docker compose)
./bin/app_startup.sh            # build, migrate, start, seed, smoke-test :8080
./bin/app_startup.sh --stop
```

or manually: `export DATABASE_URL=...` then `make run` (migrations auto-apply),
`make seed` for demo wallets.

## How to Test

```bash
make test              # unit + in-memory e2e, -race, coverage → coverage/cover.out
make test-integration  # + //go:build integration tests against $TEST_DATABASE_URL
make fmt-check && make lint
```

Covers: transfer execution, idempotent replay + key-reuse conflict, ledger
balancing, insufficient-funds failure, validation, and concurrency (in-memory
and real-Postgres).

## Tradeoffs / Assumptions

- Single currency per transfer; wallets are pre-created (no wallet API).
- Stored balance (not ledger-derived) for O(1) reads.
- Synchronous transfer, no outbox/eventing.
- Hand-rolled embedded migrator (forward-only, no down/checksum) — enough for
  this scope; `golang-migrate` wired in the Makefile for anything richer.
- **PostgreSQL is the only real persistence adapter and owns every concurrency
  guarantee; `internal/repository/memory` is a test fake, not wired into the
  server — it exists so the service and end-to-end tests stay hermetic and run
  in CI without a database.**

## Checklist

- [x] Tests pass (`make test`, `make test-integration`)
- [x] Lint passes (`golangci-lint`)
- [x] Format check passes (`gofmt`)
- [x] README / design note updated
- [x] PR description explains schema, idempotency, and concurrency

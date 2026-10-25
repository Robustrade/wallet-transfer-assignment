# Wallet Transfer Service: Understanding & Approach

> Status: **design note, written before implementation** (documentation-first workflow, per `ASSIGNMENT.md`).
> The implementation followed on this same branch / PR. Where building it changed the plan, the section is
> updated and the change is listed in [section 13](#13-changes-made-while-implementing).

## 1. Problem understanding

Build a small service for **wallet-to-wallet transfers** where the hard parts are correctness, not features:

| Concern | What must hold |
|---|---|
| Idempotency | Same `idempotencyKey` → same result, never a second transfer. Exactly-once *at the API level*. |
| Atomicity | A transfer either fully happens (balances + ledger + state) or not at all. |
| Double-entry ledger | Exactly two entries per processed transfer (one `DEBIT`, one `CREDIT`), always balanced. |
| Concurrency | No double spend, no lost updates, no deadlocks under concurrent debits on one wallet. |
| State machine | `PENDING → PROCESSED` or `PENDING → FAILED`; safe under retries and duplicates. |

Out of scope (noted as future work): auth, multi-currency, idempotency-key TTL/cleanup, async processing, sharding.

## 2. Stack

- **Go 1.24** (matches the repo's CI), `net/http` (Go 1.22+ `ServeMux` patterns), no web framework.
- **PostgreSQL** via `pgx/v5` + `pgxpool`; plain SQL migrations.
- `log/slog` for structured logs; `testify` for assertions.
- Money is `int64` **minor units**. No floats anywhere.

## 3. Architecture (layered)

```
cmd/server/            wiring, config, graceful shutdown
internal/domain/       entities, state machine, validation, sentinel errors (pure; no I/O)
internal/service/      TransferService: idempotency + workflow + transaction orchestration
internal/repository/   interfaces (TxManager, WalletRepo, TransferRepo, LedgerRepo, IdempotencyRepo)
internal/repository/postgres/   implementations
internal/handler/      HTTP: decode, validate, map errors → status codes
migrations/            SQL schema
```

| Layer | Owns | Must NOT |
|---|---|---|
| Handler | JSON decode, input shape validation, error→HTTP mapping | contain business rules |
| Service | transaction boundary, idempotency flow, funds check, state transitions | build SQL |
| Repository | SQL, locking statements, row mapping | make workflow decisions |
| Domain | `Transfer.MarkProcessed/MarkFailed`, amount/self-transfer validation | touch the DB |

## 4. Schema (PostgreSQL)

```sql
wallets(
  id TEXT PRIMARY KEY,
  balance BIGINT NOT NULL CHECK (balance >= 0),   -- DB-level overdraft guard
  created_at, updated_at)

transfers(
  id UUID PRIMARY KEY,
  idempotency_key TEXT NOT NULL UNIQUE,
  from_wallet_id TEXT NOT NULL REFERENCES wallets(id),
  to_wallet_id   TEXT NOT NULL REFERENCES wallets(id),
  amount BIGINT NOT NULL CHECK (amount > 0),
  status TEXT NOT NULL CHECK (status IN ('PENDING','PROCESSED','FAILED')),
  failure_reason TEXT,
  created_at, updated_at,
  CHECK (from_wallet_id <> to_wallet_id))

ledger_entries(                                    -- append-only
  id BIGSERIAL PRIMARY KEY,
  transfer_id UUID NOT NULL REFERENCES transfers(id),
  wallet_id TEXT NOT NULL REFERENCES wallets(id),
  type TEXT NOT NULL CHECK (type IN ('DEBIT','CREDIT')),
  amount BIGINT NOT NULL CHECK (amount > 0),
  created_at,
  UNIQUE (transfer_id, type))                      -- max one DEBIT + one CREDIT per transfer

idempotency_records(
  key TEXT PRIMARY KEY,
  request_hash TEXT NOT NULL,                      -- sha256 of the business payload
  transfer_id UUID REFERENCES transfers(id),       -- replay = load this transfer
  created_at)
```

Indexes: `ledger_entries(wallet_id, id)` (history + reconciliation), `ledger_entries(transfer_id)`,
`transfers(from_wallet_id, created_at)`. Uniqueness constraints provide the idempotency indexes.
Ledger immutability is enforced with a trigger rejecting `UPDATE`/`DELETE`.

**Balance model:** stored balance (O(1) reads, enforceable `CHECK >= 0`) **plus** the ledger as the audit
source of truth. Invariant, asserted in tests: `balance = opening balance + Σ credits − Σ debits` per wallet.

## 5. Transfer flow (single DB transaction, `READ COMMITTED`)

1. **Validate** input before touching the DB (amount > 0, wallets differ, key present).
2. **Claim key:** `INSERT INTO idempotency_records … ON CONFLICT (key) DO NOTHING`.
   - Conflict + different `request_hash` → `422 IDEMPOTENCY_KEY_REUSED`.
   - Conflict + same hash → replay stored response. A *concurrent* duplicate blocks on the unique index
     until the first transaction commits/rolls back, then sees the result (or takes over cleanly).
3. **Lock both wallets in deterministic order:**
   `SELECT … FROM wallets WHERE id = ANY($1) ORDER BY id FOR UPDATE` → A→B and B→A cannot deadlock.
   A missing wallet returns `ErrWalletNotFound` (rolling back the key claim; see section 13).
4. Insert the transfer as `PENDING` and link it to the idempotency record.
5. `balance < amount` → `FAILED(INSUFFICIENT_FUNDS)` (guarded transition, no ledger rows).
6. Success: debit source, credit destination, insert 2 ledger rows, and transition
   `PENDING → PROCESSED` with a guarded `UPDATE … WHERE status = 'PENDING'`.
7. **Commit** (key + transfer + ledger + balances are atomic). A replay loads the linked transfer.

**Business failures are recorded, not rolled back:** an insufficient-funds failure commits a `FAILED` transfer
with no ledger rows and replays the *same* `422` on retry (a failed attempt under a key is a final result;
use a new key to try again). **Infrastructure errors** (timeout, deadlock `40P01`, serialization `40001`) roll
back everything, including the key claim, so a retry is a clean first attempt; `WithinTx` retries bounded
times with backoff for the transient ones.

## 6. Why this strategy

- **Pessimistic row locks over optimistic locking:** wallets can be hot accounts; optimistic retry loops
  degrade under contention. `FOR UPDATE` serialises only the two wallets involved.
- **`READ COMMITTED` + explicit locks over `SERIALIZABLE`:** correctness comes from the locks and constraints
  we can reason about, with no blanket serialization-failure retries.
- **DB enforces invariants** (`CHECK balance >= 0`, unique keys, FKs, `UNIQUE(transfer_id,type)`), so a bug in
  application logic cannot silently corrupt money.
- **Idempotency record committed atomically with the effect**, so "response lost after commit" is safe, and it
  survives process restarts (durable, not in-memory).

## 7. Edge cases & failure modes

| Scenario | Behaviour |
|---|---|
| Sequential duplicate | Stored response replayed (`200`, header `Idempotent-Replayed: true`). |
| Concurrent duplicates (same key) | Unique index serialises; exactly one transfer, identical responses. |
| Same key, different payload | `422 IDEMPOTENCY_KEY_REUSED`. |
| Two debits on one wallet | Serialised by `FOR UPDATE`; later one sees updated balance → insufficient funds. |
| A→B and B→A simultaneously | Ordered locking, no deadlock. |
| Self-transfer / amount ≤ 0 / overflow | Rejected in domain; DB `CHECK`s as backstop. |
| Unknown wallet | `404`, nothing recorded (the key is not consumed; the wallet may be created later). |
| Crash mid-transaction | Rollback; no partial state. |
| Crash after commit, response lost | Client retries same key → stored result returned. |
| Stuck `PENDING` | Cannot be committed in the single-tx design (documented; async split would need a reaper). |
| Client disconnect / timeout | `context` cancellation rolls back the transaction. |

## 8. API

`POST /transfers`, body `{ "idempotencyKey", "fromWalletId", "toWalletId", "amount" }`
(`Idempotency-Key` header accepted as a fallback; the body field is the contract).

| Status | Meaning |
|---|---|
| `201` | Transfer processed (first execution) |
| `200` | Idempotent replay of an earlier result |
| `400` | Malformed / invalid request |
| `404` | Wallet not found |
| `422` | Insufficient funds, or key reused with a different payload |

Optional, kept deliberately small: `GET /wallets/{id}` (balance), `GET /transfers/{id}`, `GET /healthz`.

## 9. Testing strategy (Red → Green → Refactor, visible in commit order)

- **Domain unit tests:** state machine, validation.
- **Handler tests** with fake services pin the HTTP contract.
- **Service and repository tests on real Postgres** (embedded automatically, or `TEST_DATABASE_URL`):
  - happy path: 2 ledger entries, balances correct, ledger balanced
  - idempotent replay: no second ledger write; same-key-different-payload rejected
  - insufficient funds → `FAILED`, no balance change, replayed on retry
  - **concurrency:** N goroutines spending from one wallet funded for fewer than N → exactly the affordable count succeed
  - **concurrent duplicates:** many goroutines, same key → one transfer, identical responses
  - **opposing transfers** A↔B in a loop → no deadlock
  - reconciliation invariant checked after every test
- Run with `-race`; `make lint`, `make fmt-check`, `make test` mirror the CI variables.

## 10. Observability

`slog` structured logs (request id, transfer id, hashed idempotency key, outcome, replay flag) and a few
counters (transfers by outcome, replays, tx retries) if time allows.

## 11. Assumptions & trade-offs

- Idempotency keys are globally unique here; a real system would scope them per client/tenant and expire them.
- A failed business outcome is final for its key (exactly-once semantics) rather than retryable.
- Single-region, single Postgres primary; cross-service distributed transactions (sagas/outbox) are out of scope.
- Synchronous processing: simpler and fully atomic, at the cost of holding row locks for the transaction's duration.

## 12. Delivery plan (topical commits), as originally planned

1. `docs:` approach (this PR's first commit)
2. `chore:` project scaffold, Makefile, lint config, docker-compose
3. `feat:` schema & migrations
4. `feat:` domain model & state machine (+ tests)
5. `feat:` repository layer (Postgres)
6. `feat:` transfer service: transactional transfer logic (+ tests)
7. `feat:` idempotency handling (+ tests)
8. `test:` concurrency & reconciliation tests
9. `feat:` HTTP handlers
10. `docs:` README, PR notes, AI usage disclosure & session transcript

## 13. Changes made while implementing

| Planned | Built | Why |
|---|---|---|
| `idempotency_records` stored `response_status` / `response_body` | Stores only `request_hash` + `transfer_id`; replay loads the transfer | Keeps the HTTP shape out of the data layer and avoids a second copy of the result that could drift. |
| Unknown wallet recorded as `FAILED(WALLET_NOT_FOUND)` | Returns `404` and writes nothing | `transfers` has foreign keys to `wallets`, so a transfer to a non-existent wallet cannot be stored. Not consuming the key is also friendlier: the wallet may be created later. |
| docker-compose Postgres for tests | Embedded PostgreSQL started by the test suite | No Docker needed; `go test ./...` works anywhere (`TEST_DATABASE_URL` overrides). |
| Service tests with fake repositories | Service tests run against real Postgres | Locking and constraint behaviour cannot be faked meaningfully; the same tests exercise the repository. |
| Optional metrics counters | Structured request and outcome logs only | Kept the scope explainable. |
| Ledger balance check as an optional hardening | Implemented as a deferred constraint trigger | Cheap, and it makes "the ledger always balances" a database guarantee. |

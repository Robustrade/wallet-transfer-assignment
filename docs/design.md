# Wallet Transfer Service — Design Note

This note is written **before** the implementation and kept up to date as the code evolves. It
records the contract, the guarantees and the reasoning behind the main decisions.

## 1. Problem statement

Move money between two wallets with:

- exactly-once effect at the API level when an `idempotencyKey` is supplied
- a double-entry ledger (one DEBIT + one CREDIT per processed transfer)
- correct balances under concurrent requests (no double spend)
- a safe transfer state machine (`PENDING -> PROCESSED | FAILED`)

## 2. Tech choices

| Concern     | Choice                                         | Why |
|-------------|------------------------------------------------|-----|
| Language    | Java 21, Spring Boot 3.5, Gradle               | Mainstream JVM stack; records and sealed types keep the domain small |
| Database    | PostgreSQL 16                                  | Preferred by the assignment; row-level locks and `ON CONFLICT` give us the primitives we need |
| Migrations  | Flyway (`src/main/resources/db/migration`)     | Versioned, reviewable schema |
| Data access | Spring `JdbcClient` (plain SQL)                | The SQL *is* the concurrency design (locks, guarded updates); an ORM would hide it |
| Tests       | JUnit 5, AssertJ, MockMvc, Testcontainers      | Tests run against a **real PostgreSQL**, so locking and constraint behaviour is genuinely exercised |
| Money       | `long` minor units (e.g. cents)                | No floating point; `amount > 0` enforced in the domain **and** by a `CHECK` |
| IDs         | UUIDs for transfers and ledger rows            | Generated in the service layer |

## 3. API contract

### `POST /transfers`

```json
{ "idempotencyKey": "abc123", "fromWalletId": "wallet_1", "toWalletId": "wallet_2", "amount": 100 }
```

| Outcome                                    | Status | Body |
|--------------------------------------------|--------|------|
| Transfer processed (first time)            | 201    | transfer |
| Replay of a processed transfer             | 200    | the **original** transfer, header `Idempotent-Replayed: true` |
| Insufficient funds (first time and replay) | 422    | transfer with `state: FAILED`, `failureReason: INSUFFICIENT_FUNDS` |
| Validation error (amount <= 0, same wallet, missing field, bad JSON, unknown field) | 400 | error |
| Wallet not found                           | 404    | error |
| Same key, **different** payload            | 409    | error `IDEMPOTENCY_KEY_REUSED` |

`idempotencyKey` is optional. Without it the request is executed every time (no deduplication is
possible; that is the documented semantic).

Error body: `{ "error": { "code": "...", "message": "..." } }`.

### Supporting endpoints

- `POST /wallets` `{ "id": "wallet_1", "openingBalance": 1000 }` — create a wallet (201, 409 if it exists).
- `GET /wallets/{id}` — wallet with current balance (optional enhancement).
- `GET /transfers/{id}` — fetch a transfer.
- `GET /actuator/health` — liveness/readiness.

Wallets must exist before they can be transferred between. `openingBalance` is funded from
*outside* the system (see assumptions).

## 4. Data model

```text
wallets             (id PK, balance >= 0, opening_balance >= 0, created_at, updated_at)
transfers           (id PK, idempotency_key, from_wallet_id FK, to_wallet_id FK, amount > 0, state,
                     failure_reason, created_at, updated_at, CHECK from <> to, CHECK state IN (...))
ledger_entries      (id PK, transfer_id FK, wallet_id FK, type DEBIT|CREDIT, amount > 0, created_at,
                     UNIQUE (transfer_id, type))
idempotency_records (key PK, request_hash, transfer_id FK UNIQUE DEFERRABLE INITIALLY DEFERRED, created_at)
```

Integrity the **database** enforces (not just application code):

- `balance >= 0` — a debit that would overdraw fails even if application logic is buggy.
- `UNIQUE (transfer_id, type)` — at most one DEBIT and one CREDIT per transfer.
- `idempotency_records.key` is the primary key — two racing requests with one key cannot both claim it.
- Foreign keys — ledger rows cannot exist without a transfer / wallet.
- `CHECK` constraints on amount, state, `from <> to`, and "failure reason present iff FAILED".
- Ledger rows are append-only (a trigger rejects `UPDATE`/`DELETE`).

Indexes: `ledger_entries(wallet_id, created_at)` for history and reconciliation,
`transfers(from_wallet_id, created_at)` / `transfers(to_wallet_id, created_at)` for per-wallet
listing. Primary keys cover the idempotency lookup.

Balance strategy: **stored balance**, updated in the same transaction as the ledger entries.
Invariants checkable at any time (and asserted by the tests after every scenario):

```text
wallet.balance = wallet.opening_balance + SUM(credits) - SUM(debits)
SUM(all DEBIT amounts) = SUM(all CREDIT amounts)
```

## 5. State machine

```text
PENDING --> PROCESSED
PENDING --> FAILED
```

`PROCESSED` and `FAILED` are terminal. Transitions are enforced twice: in the domain
(`Transfer.processed()/failed()` reject anything but `PENDING`) and in SQL
(`UPDATE ... WHERE id = ? AND state = 'PENDING'`, rows-affected checked), so a duplicate or retried
worker cannot flip a terminal transfer.

## 6. Transaction and concurrency strategy

The whole workflow runs in **one database transaction** at `READ COMMITTED`:

1. claim the idempotency key (if any) — see §7; replay and finish if it already exists
2. lock both wallets: `SELECT id FROM wallets WHERE id IN (:from, :to) ORDER BY id FOR UPDATE`
   (a missing row means unknown wallet → 404 and the whole transaction, including the claim, rolls back)
3. insert the `transfers` row (`PENDING`)
4. guarded debit: `UPDATE wallets SET balance = balance - :amt WHERE id = :from AND balance >= :amt`
   - 0 rows affected → insufficient funds → mark `FAILED`, commit (the outcome is recorded)
5. credit: `UPDATE wallets SET balance = balance + :amt WHERE id = :to`
6. insert the DEBIT and CREDIT ledger entries
7. mark `PROCESSED`, commit

Why this is safe:

- **Pessimistic row locks, taken in a fixed order.** `FOR UPDATE ... ORDER BY id` serialises
  transfers that touch the same wallet and makes `A->B` racing with `B->A` deadlock-free, because
  everyone locks the lower id first.
- **No read-then-write.** The funds check and the decrement are one atomic statement; even
  without the lock two concurrent debits could not both observe the old balance. The `CHECK
  (balance >= 0)` is the last line of defence.
- **Why pessimistic rather than optimistic locking?** Wallets are hot rows, and under contention
  optimistic versioning means retry storms. Locks make the common case a short wait and keep the
  logic simple. Transfers between disjoint wallets still run fully in parallel.
- A crash before commit rolls everything back; there is no partially applied transfer. A crash
  after commit is covered by idempotency (the client retries with the same key and receives the
  stored result).

## 7. Idempotency

- **Storage:** `idempotency_records(key PK, request_hash, transfer_id)`, written in the *same*
  transaction as the transfer, so a record exists iff the transfer does. The FK to `transfers` is
  `DEFERRABLE INITIALLY DEFERRED` so the key can be claimed first and the transfer inserted later
  in the same transaction.
- **Detection and race handling:** `INSERT ... ON CONFLICT (key) DO NOTHING`.
  - 1 row inserted → this request owns the key and executes the transfer.
  - 0 rows → another request owns it: load its record and return the original result.
  - If two requests race with a new key, PostgreSQL makes the second `INSERT` **wait** on the
    unique index until the first commits or rolls back. After a commit the second sees a conflict
    and replays; after a rollback it simply becomes the owner. No application-level retry loop or
    exception juggling is needed.
- **Same result:** replay returns the stored transfer (including a stored `FAILED`).
- **Payload mismatch:** `request_hash` = SHA-256 over (from, to, amount); a different payload under
  the same key → 409 rather than silently returning an unrelated result.
- **No duplicate side effects:** a replay never reaches the lock/debit/credit steps.
- **Restarts:** all state is in the database, so this survives process restarts.
- **Failed transfers are final for that key.** To retry after insufficient funds the client uses a
  new key (the usual convention for idempotent APIs).

## 8. Failure modes

| Failure | Behaviour |
|---------|-----------|
| Client times out, request actually committed | Retry with same key returns the original result |
| Crash mid-transaction | Rolled back; retry executes normally |
| Two identical requests race | One executes, the other waits and replays its result |
| Two different transfers debit the same wallet | Serialised by the row lock; the later one fails with `INSUFFICIENT_FUNDS` if funds ran out |
| `A->B` and `B->A` at the same time | Locks taken in id order, no deadlock |
| Unknown wallet | 404, nothing persisted |
| Same key, different payload | 409, nothing changed |
| Database unavailable | 500 with an opaque body; nothing partially applied |

## 9. Assumptions and trade-offs

- Single currency, integer minor units.
- `openingBalance` is funded outside the system; deposits/withdrawals are out of scope.
- No authentication, so idempotency keys are global rather than per-caller (in production the
  unique key would be `(client_id, key)`).
- Idempotency records never expire (no TTL job).
- Row locks limit throughput per *wallet*, not globally. A single extremely hot wallet is a
  bottleneck by nature (one balance); sharding or batching would be the next step.

## 10. Observability

- `key=value` log lines at the service boundary: one INFO line per outcome (`transfer processed`,
  `transfer failed` with the reason, `transfer replayed` with the key and stored state), carrying
  ids and amounts only. Unexpected exceptions are logged at ERROR with the stack trace and returned
  to the client as an opaque 500. Request bodies are not logged.
- `GET /actuator/health` (the only exposed Actuator endpoint) reports liveness including the
  database check. Metrics and tracing are out of scope.

## 11. Testing strategy (TDD: red → green → refactor)

- **Domain:** state-machine transitions, validation rules (plain unit tests, no Spring).
- **Repository:** against a real PostgreSQL (Testcontainers): constraints, guarded debit, uniqueness, rollback.
- **Service:** behaviour-level: happy path, ledger has two balanced entries, insufficient funds is
  recorded as `FAILED`, replay returns the original result without moving money twice, payload
  mismatch, unknown wallet.
- **Concurrency:** N threads debiting one wallet (balance never negative, exactly
  `floor(balance/amount)` succeed, ledger balances); N threads using the **same** idempotency key
  (exactly one transfer, one pair of ledger rows); opposing transfers (no deadlock).
- **API:** MockMvc against the full stack for status codes and JSON.
- **Invariant helper** used after every scenario: balances reconcile with the ledger.

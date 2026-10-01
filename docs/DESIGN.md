# Wallet Transfer Service — Design Note

## Problem statement

Expose a transfer API that moves value from one wallet to another with:

- exactly-once behavior at the API when `idempotencyKey` is present
- an always-balanced double-entry ledger
- correct balances under concurrent debit of the same wallet
- retry-safe state transitions (`PENDING` → `PROCESSED` or `PENDING` → `FAILED`)

The service must remain correct under duplicate delivery, client retries, and process crashes. A crash must not commit a partial transfer.

## Expected behavior

| Scenario | Result |
| --- | --- |
| Valid transfer, new `idempotencyKey` | Debit source, credit destination, two ledger rows, transfer `PROCESSED` |
| Same key, same payload, after success | Original transfer body, no new ledger rows, no balance change |
| Same key, same payload, after insufficient funds | Original `FAILED` transfer body, no ledger rows |
| Same key, different payload | `409 Conflict`, no side effects |
| Concurrent transfers from the same wallet | Serialised per wallet; never overdraft; ledger stays balanced |
| Invalid input (missing key, amount ≤ 0, same wallet) | `400`, no idempotency row, no transfer |
| Unknown wallet | `404`, no transfer, key is **not** stored (same as validation: the request was never accepted) |

Amounts are integers in the smallest currency unit (for this assignment, whole units as given in the spec). Floating-point JSON amounts are rejected.

## API contract

### `POST /wallets`

Creates a wallet for setup and tests (not in the assignment’s required surface, but required to exercise transfers).

```json
{ "id": "wallet_1", "initialBalance": 1000 }
```

- `201` on create
- `409` if the wallet already exists

### `GET /wallets/{id}`

Returns `{ "id", "balance" }`. Optional in the assignment; included so balances are observable.

### `POST /transfers`

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

Success (`201` first time, `200` on replay):

```json
{
  "id": "<uuid>",
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100,
  "status": "PROCESSED",
  "failureReason": null,
  "ledger": [
    { "walletId": "wallet_1", "type": "DEBIT", "amount": 100 },
    { "walletId": "wallet_2", "type": "CREDIT", "amount": 100 }
  ]
}
```

Insufficient funds (`422` first time and on replay): same shape with `status: "FAILED"` and `failureReason: "INSUFFICIENT_FUNDS"`, `ledger: []`.

`idempotencyKey` is required. Replays return the stored transfer, not a newly computed one.

## Side effects of a successful transfer (single database transaction)

1. Claim the idempotency key (insert `idempotency_records`).
2. Insert `transfers` in `PENDING`.
3. Insert two `ledger_entries` (DEBIT source, CREDIT destination).
4. Update both wallet balances.
5. Set transfer to `PROCESSED`.
6. Mark the idempotency row `COMPLETED` with `transfer_id`.

All six steps commit together or not at all. After commit the transfer is never left in `PENDING`. `PENDING` exists so the state machine is explicit inside the transaction; it is not a durable in-flight status.

A failed funding check inserts `PENDING`, then updates the same row to `FAILED` in that transaction, with **no** ledger entries and **no** balance change.

## Failure modes

| Failure | Handling |
| --- | --- |
| Process crash mid-transaction | Rollback; client retry with the same key starts a new attempt |
| Duplicate HTTP request after commit | Unique key lookup returns the original transfer |
| Duplicate request while first transaction is open | Second writer waits on the database write lock, then hits the unique key and reads the committed result |
| Insufficient funds | Durable `FAILED` transfer; replay is stable |
| Idempotency key reused with different amount/wallets | `409`; stored hash mismatch |
| Debit and credit wallets locked in inconsistent order | Wallets are locked in lexicographic `id` order to avoid deadlocks |

There is no out-of-band retry worker. Recovery is client retry plus a single atomic transaction.

## Idempotency behavior

- **Storage:** `idempotency_records.key` is the primary key (`TEXT`).
- **Fingerprint:** SHA-256 of `fmt.Sprintf("%q\n%q\n%d", fromWalletId, toWalletId, amount)` (Go-quoted wallet IDs so a newline in an id cannot alias another field). Stored as `request_hash`.
- **Detection:** the service `INSERT`s the claim at the start of the transfer transaction via the store. A unique-constraint violation means this key already completed (or is visible after the other transaction committed).
- **Original result:** `transfer_id` on the idempotency row is loaded and returned as JSON. HTTP status follows the original outcome (`200` + `PROCESSED`, or `422` + `FAILED`).
- **No duplicate side effects:** the unique insert is the first write in the transaction. A second request cannot insert a second transfer for that key.
- **Unknown wallet / invalid input:** these never complete an idempotency row. The client may reuse the key once the request is actually acceptable.

`PROCESSING` is written in the same uncommitted transaction as the transfer work and is only committed after `COMPLETED` + `transfer_id` are set. Callers never observe a durable `PROCESSING` row.

## Retry behavior

- Safe to retry any `POST /transfers` with the same key and body.
- Timeouts after the server committed still return the original transfer on retry (exactly-once at the API).
- Changing the body but keeping the key is a client error (`409`), not a new transfer.

## Consistency expectations

- **Atomicity:** one DB transaction per transfer attempt.
- **Ledger:** for every `PROCESSED` transfer, `SUM(CREDIT) = SUM(DEBIT) = amount`. A unique index on `(transfer_id, entry_type)` allows at most one DEBIT and one CREDIT per transfer.
- **Balances:** stored on `wallets.balance` with `CHECK (balance >= 0)`. The stored balance is the source of truth for authorization; the ledger is the audit log. They are updated in the same transaction, so they cannot diverge on commit.
- **Concurrency:** SQLite `BEGIN IMMEDIATE` plus ordered wallet reads serialise writers. The same workflow maps to PostgreSQL `SELECT ... FOR UPDATE` on wallet rows (documented below). Overdraft is impossible because the balance check and decrement occur under the write lock, and the check constraint is a backstop.

## Observability expectations

- Structured logs (`slog`) for transfer start, successful finish, and failed attempts (`idempotencyKey`, wallet ids, amount, `replay`, `err` or `status`). Payloads beyond those fields are not logged.
- `GET /health` for liveness.
- Metrics and tracing are out of scope.

## Database schema

SQLite is used so `go test` and CI run without Postgres. The model is PostgreSQL-shaped and would use `BIGINT`, `TIMESTAMPTZ`, and `FOR UPDATE` in production.

- `wallets(id PK, balance NOT NULL CHECK >= 0, created_at)`
- `transfers(id PK, from_wallet_id FK, to_wallet_id FK, amount CHECK > 0, status IN PENDING|PROCESSED|FAILED, failure_reason, created_at, updated_at)`
- `ledger_entries(id PK, transfer_id FK, wallet_id FK, entry_type IN DEBIT|CREDIT, amount CHECK > 0, created_at)` unique `(transfer_id, entry_type)`
- `idempotency_records(key PK, request_hash NOT NULL, transfer_id FK NULL, status, created_at, updated_at)`

Indexes: `ledger_entries(wallet_id)`, `ledger_entries(transfer_id)`, `transfers(from_wallet_id)`.

## Concurrency strategy (choice)

**Pessimistic locking inside a single transaction**, not optimistic versions.

- SQLite: `BEGIN IMMEDIATE` (write lock before reads that will write).
- PostgreSQL equivalent: `BEGIN` then `SELECT * FROM wallets WHERE id IN ($1,$2) ORDER BY id FOR UPDATE`.

Optimistic locking (`version` column) is valid but turns every conflict into a retry loop in the application. Row/database locking keeps the service code linear and makes double-spend a database problem, which is easier to prove.

## Layering

| Layer | Responsibility |
| --- | --- |
| `internal/httpapi` | JSON decode, validation, status codes |
| `internal/service` | idempotency, workflow, state transitions |
| `internal/store` | SQL, transactions, constraints |
| `internal/domain` | entities, statuses, errors |

Handlers do not open transactions. The store exposes `WithTx` plus claim/load/complete and row operations; **idempotency policy** (replay vs conflict vs new attempt) lives in the service.

## Testing strategy

Behavioral tests (HTTP + real SQLite file DB):

1. Happy-path transfer: balances and two ledger rows.
2. Idempotent replay: same `id`, unchanged balances.
3. Key reuse with different payload: `409`.
4. Insufficient funds: `FAILED`, no ledger, replay stable.
5. Concurrent transfers against one funded wallet: final balance ≥ 0, no extra ledger rows, total debited equals starting balance minus remaining.
6. Validation errors do not create transfers.

Red/blue/green: tests are written to assert the contract above; implementation follows.

## Assumptions and tradeoffs

- Integer amounts only.
- Wallets must be created before transfer; no implicit wallet creation.
- `FAILED` transfers have zero ledger entries (a failed movement is not recorded as a movement).
- SQLite serialises all writers. That is stricter than Postgres row locks but is correct and CI-friendly. Production would switch the store dialect, not the service.
- No auth, no multi-tenancy, no currency field.
- No async outbox; the HTTP request is the transaction boundary.

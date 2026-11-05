# Wallet Transfer Service — Design Note

This note is written before the code. It describes what the service does, how it
stays correct under retries and concurrent requests, and how it is tested.

## 1. Problem statement

Move money from one wallet to another. Every transfer must:

- happen at most once per `idempotencyKey` (exactly-once at the API level),
- write two ledger entries (one DEBIT, one CREDIT) that always balance,
- keep wallet balances correct when many transfers run at the same time,
- move through a clear state machine: `PENDING -> PROCESSED` or `PENDING -> FAILED`.

## 2. Stack

- Go 1.24, standard library `net/http` (Go 1.22+ routing), `log/slog` for structured logs
- `database/sql` with `github.com/lib/pq` (the only external dependency)
- PostgreSQL 16 — correctness rules are enforced in the database where possible
- `go test` with `-race`; integration tests run against real Postgres, because locking
  behaviour cannot be tested with an in-memory fake

## 3. API contract

### `POST /transfers`

Request:

```json
{ "idempotencyKey": "abc123", "fromWalletId": "wallet_1", "toWalletId": "wallet_2", "amount": 100 }
```

- `amount` is a positive integer in minor units (for example paise/cents). No floats.
- `fromWalletId` and `toWalletId` must be different.

Responses:

| Case | HTTP | Body |
|------|------|------|
| Transfer done | `201` | transfer with `status: "PROCESSED"` |
| Not enough balance | `422` | transfer with `status: "FAILED"`, `failureReason: "INSUFFICIENT_FUNDS"` |
| Same key, same body (replay) | same code and body as the first call | header `Idempotent-Replayed: true` |
| Same key, different body | `409` | `IDEMPOTENCY_KEY_REUSED` |
| Invalid body | `400` | `VALIDATION_ERROR` |
| Wallet does not exist | `404` | `WALLET_NOT_FOUND` |

`400` and `404` are rejected **before** anything is stored, so the key is not used up.
`422` is a real business outcome: it is stored and replayed like a success.

### Supporting endpoints (optional extras)

- `POST /wallets` — create a wallet with an opening balance (test/demo helper only)
- `GET /wallets/{id}` — current balance
- `GET /wallets/{id}/ledger` — ledger entries of a wallet
- `GET /transfers/{id}` — one transfer
- `GET /health` — DB check

## 4. Schema

```
wallets              (id PK, balance BIGINT CHECK >= 0, created_at, updated_at)
transfers            (id UUID PK, from_wallet_id FK, to_wallet_id FK, amount BIGINT CHECK > 0,
                      status CHECK IN (PENDING, PROCESSED, FAILED), failure_reason,
                      CHECK from <> to, CHECK failure_reason only when FAILED)
ledger_entries       (id PK, transfer_id FK NOT NULL, wallet_id FK, type CHECK IN (DEBIT, CREDIT),
                      amount BIGINT CHECK > 0, UNIQUE (transfer_id, type))
idempotency_records  (idempotency_key PK, request_hash, transfer_id FK UNIQUE,
                      result JSONB -- snapshot of the final transfer, NULL while PENDING)
```

Database-level guards:

- `wallets.balance >= 0` — a last safety net against double spending.
- `UNIQUE (transfer_id, type)` — a transfer can never get two debits or two credits.
- A deferred constraint trigger checks at commit time that every transfer with ledger
  rows is `PROCESSED`, has exactly one DEBIT (source wallet) and one CREDIT
  (destination wallet), and both amounts equal the transfer amount.
- Ledger is append-only (trigger rejects UPDATE/DELETE).
- `idempotency_records.idempotency_key` primary key — the dedup guarantee lives in the DB,
  so it survives process restarts and works across many app instances.
- Indexes: `ledger_entries(wallet_id, id)` for ledger history, `transfers(from_wallet_id)`
  and `transfers(to_wallet_id)` for history lookups.

Balance choice: **stored balance**, updated in the same transaction as the ledger rows.
Reads are O(1). The ledger is still the audit trail, and a test checks that
`balance == opening balance + credits - debits` for every wallet.

## 5. Flow and transaction boundaries

The work is split into two short transactions. This makes `PENDING` a real, durable
state and makes a crash in the middle safe.

**Step A — reserve the key (transaction 1)**

1. Validate the body (handler + domain). Hash `{fromWalletId, toWalletId, amount}` (SHA-256).
2. Fast path: if the key already exists → compare hash (`409` if different), go to Step B.
3. Check both wallets exist (`404` otherwise, nothing stored).
4. `INSERT INTO transfers (... status='PENDING')` and
   `INSERT INTO idempotency_records ... ON CONFLICT (idempotency_key) DO NOTHING`.
5. If the insert hit a conflict → roll back and read the winner's record (same checks as 2).
6. Commit.

**Step B — process the transfer (transaction 2, safe to run many times)**

1. `SELECT ... FROM transfers WHERE id = $1 FOR UPDATE`.
2. If status is not `PENDING` → it is already done; return the stored result.
3. Lock both wallets with `SELECT ... ORDER BY id FOR NO KEY UPDATE` (fixed order → no deadlocks).
4. If source balance < amount → `UPDATE transfers SET status='FAILED' WHERE id=$1 AND status='PENDING'`.
5. Else → update both balances, insert DEBIT + CREDIT, set status `PROCESSED`
   (same guarded `WHERE status='PENDING'` update).
6. Save a snapshot of the final transfer into `idempotency_records.result`.
   The handler maps `PROCESSED` → `201` and `FAILED` → `422`, so a replay gives the same code and body.
7. Commit.

## 6. Idempotency behaviour

| Situation | What happens |
|-----------|--------------|
| Same request sent twice, one after another | 2nd call finds the record, Step B sees a final state, returns the saved result |
| Same request sent twice at the same moment | Both try to insert the key; Postgres makes the 2nd wait on the unique index, then it sees the conflict. It then waits on the transfer row lock in Step B and returns the saved result |
| 1st request committed but the response was lost | The client retries with the same key and gets the saved result. No new money moves |
| Process crashed after Step A, before Step B | Transfer stays `PENDING`. A retry with the same key runs Step B and finishes it |
| Same key, different body | `409`, nothing changes |

## 7. Concurrency strategy

- **Pessimistic row locks** (`SELECT ... FOR NO KEY UPDATE`) on both wallets inside one transaction,
  at the default `READ COMMITTED` level. The balance check and the balance update happen
  while holding the lock, so there is no read-then-write race.
- Locks are always taken in **sorted wallet id order**, so A→B and B→A at the same time
  cannot deadlock.
- `FOR NO KEY UPDATE`, not `FOR UPDATE`: inserting a transfer makes Postgres take
  `FOR KEY SHARE` locks on both wallets (foreign-key check), in from→to order. `FOR UPDATE`
  conflicts with `FOR KEY SHARE`, so a B→A insert could deadlock with a transfer holding A.
  This was found by the concurrency test under load (about 1 run in 13) and is now covered by a
  deterministic regression test. `FOR NO KEY UPDATE` still serialises balance changes and is the
  lock Postgres itself takes for an `UPDATE` that does not change the key.
- Why not optimistic locking: transfers from a hot wallet would keep failing and retrying.
  Row locks queue them instead and keep the code simple.
- Why not `SERIALIZABLE`: it would also be correct, but needs retry loops on
  serialization errors. Row locks give the same safety for this case with less code.
- The `CHECK (balance >= 0)` constraint is a second line of defence if a future code
  change forgets a lock.

## 8. Failure modes

| Failure | Result |
|---------|--------|
| Insufficient funds | Transfer `FAILED`, stored, replayed as `422` |
| DB error inside Step B | Transaction rolls back, transfer stays `PENDING`, client gets `500`, retry finishes it |
| Unknown wallet | `404`, nothing stored |
| Same wallet as source and destination | `400` |
| Key reused with another body | `409` |

## 9. Observability

- `log/slog` JSON logs: one line per request (method, path, status, duration).
- Service logs: transfer reserved, processed, failed (with reason), idempotent replay.
- `GET /health` checks the DB connection.

## 10. Testing strategy

- **Unit tests** — domain rules: state machine transitions, amount and wallet validation.
- **Integration tests (real Postgres)**:
  - happy path: balances and two ledger entries
  - insufficient funds → `FAILED`, no ledger rows, balances unchanged
  - replay returns the same result and moves money only once
  - same key + different body → `409`
  - many parallel requests with the **same key** → one transfer, one pair of ledger rows
  - many parallel debits from one wallet → no overdraft, final balance exact
  - parallel A→B and B→A → no deadlock, money is conserved
  - ledger invariant: total debits == total credits; balance == opening + credits − debits
  - resume of a `PENDING` transfer after a simulated crash
  - DB rejects a lone ledger entry, ledger edits and negative balances
- **HTTP tests** with `httptest` for status codes, replay header and validation.
- Everything runs with `go test -race`.

## 11. Out of scope / assumptions

- Single currency; amounts are integers in minor units.
- No auth. `POST /wallets` exists only to seed data for demos and tests.
- Idempotency records are kept forever (a real system would expire them, for example after 24h).
- No background job to finish stuck `PENDING` transfers; the client retry does it.
  A sweeper job would be the next step.

# Wallet Transfer Service — Design Note

Documentation-first note per `ASSIGNMENT.md`. Update this before non-trivial code changes.

## 1. Problem statement

Provide wallet-to-wallet transfers with exactly-once semantics at the API level
(keyed by `idempotencyKey`), a double-entry ledger, correct balances under
concurrency, and a safe transfer state machine.

## 2. API contract

### `POST /transfers`

Request:

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

`amount` is a positive integer in minor units (e.g. cents).

Responses:

| Status | When |
| ------ | ---- |
| `201 Created` | transfer processed; body is the transfer resource with `state = PROCESSED` |
| `200 OK` | duplicate `idempotencyKey` — original result replayed |
| `400 Bad Request` | malformed body / missing field / non-positive amount / self-transfer |
| `409 Conflict` | same `idempotencyKey` reused with a different request payload |
| `422 Unprocessable Entity` | business rule failed (insufficient funds) — transfer stored as `FAILED` |
| `404 Not Found` | wallet does not exist |
| `500` | unexpected error |

Transfer resource:

```json
{
  "id": "txn_...",
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100,
  "currency": "USD",
  "state": "PROCESSED",
  "createdAt": "2026-09-07T12:00:00Z"
}
```

### `GET /wallets/{id}/balance` (optional enhancement)

```json
{ "walletId": "wallet_1", "balance": 99900, "currency": "USD" }
```

## 3. Side effects

A successful transfer, in one DB transaction:

1. inserts an `idempotency_records` row (claims the key),
2. inserts a `transfers` row (`PENDING`),
3. locks both wallet rows (`SELECT ... FOR UPDATE`, ordered by wallet id),
4. decrements `from` balance, increments `to` balance,
5. inserts exactly two `ledger_entries` (`DEBIT` then `CREDIT`),
6. moves the transfer `PENDING -> PROCESSED`.

On insufficient funds the transaction still commits with the transfer stored as
`FAILED` and no balance/ledger changes (the debit/credit + ledger writes are
skipped, but the `idempotency_records` and `transfers` rows persist so the
failure is replayable).

## 4. Failure modes

| Failure | Behaviour |
| ------- | --------- |
| Duplicate request, same payload | replay stored transfer, no new side effects |
| Duplicate request, different payload | `409`, nothing written |
| Insufficient funds | transfer `FAILED`, `422`, replayable |
| Crash mid-transaction | DB rolls back; key not claimed; client retry is a fresh attempt |
| Crash after commit, before response | client retry hits the idempotency fast-path and gets the stored result |
| Concurrent debits of one wallet | serialised by row lock; second waits, then sees updated balance |
| Deadlock from lock ordering | avoided — wallets are always locked in ascending id order |

## 5. Idempotency strategy

- `idempotency_records.key` is the table PRIMARY KEY. The unique constraint is the
  actual guarantee; the application-level pre-check is only an optimisation.
- Flow: (a) fast-path `SELECT` by key outside the tx; if found, return it.
  (b) Inside the tx, `INSERT` the key. A unique violation means a concurrent or
  prior request won the race — re-read and return that transfer.
- `request_hash` = hash of `(fromWalletId, toWalletId, amount, currency)`. A key
  reused with a mismatching hash returns `409` rather than silently replaying an
  unrelated result.

## 6. Concurrency & consistency

- Isolation level: `READ COMMITTED` (Postgres default) + explicit
  `SELECT ... FOR UPDATE` on both wallet rows. Pessimistic locking is chosen over
  optimistic retry because contention on a hot wallet is expected and we want
  bounded, predictable latency rather than retry storms.
- Lock acquisition order is deterministic (ascending wallet id) to prevent
  deadlocks between `A->B` and `B->A` transfers.
- Balance is a stored column with a `CHECK (balance >= 0)` backstop; the ledger
  is the audit trail and must always sum to zero across a transfer
  (`sum(DEBIT) == sum(CREDIT)`), enforced structurally by
  `UNIQUE (transfer_id, type)` + equal amounts written from one code path.

## 7. Observability

- Structured logs (`slog`) at request boundary and on state transitions, keyed by
  `transfer_id` and `idempotency_key`.
- Suggested metrics (not required): `transfers_total{state}`,
  `transfer_duration_seconds`, `idempotency_hits_total`.

## 8. Testing strategy

Inside-out, Red → Green → Refactor:

1. `internal/domain` — state machine + money math, pure unit tests.
2. `internal/service` — workflow orchestration against the in-memory port fake
   (`internal/repository/memory`): idempotency replay, key-reuse conflict,
   failure path, ledger balance.
3. `internal/repository/postgres` — `//go:build integration`, real Postgres
   (`TEST_DATABASE_URL`): `SELECT ... FOR UPDATE` serialisation under 50
   concurrent debits, unique-constraint dedup, atomic ledger pair. This is the
   authoritative concurrency test.
4. `internal/handler/http` — `httptest`, service mocked: status/body mapping.
5. `test/e2e` — full HTTP stack over the in-memory fake; N concurrent debits of
   one wallet → exactly the affordable number succeed, final balance correct.

## 9. Assumptions / tradeoffs

- Single currency per transfer; cross-currency is out of scope.
- Wallets are pre-created (`make seed` / `cmd/seeddev` for dev); no
  wallet-creation API.
- Stored balance (not ledger-derived) for O(1) reads; ledger reconciliation is a
  separate concern.
- No outbox/event publishing; the transfer is fully synchronous within the
  request.
- PostgreSQL is the only real persistence adapter and owns every concurrency
  guarantee (per-transfer transaction, `SELECT ... FOR UPDATE`, unique
  constraints, deadlock retry). `internal/repository/memory` is a test fake, not
  wired into the server — it exists so the service and end-to-end tests stay
  hermetic and run in CI without a database.

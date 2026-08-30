# Wallet Transfer Service — Design

## Problem statement

Move value from one wallet to another with API-level exactly-once behavior when an `idempotencyKey` is supplied. Concurrent transfers against the same wallet must not double-spend. Every successful transfer is recorded as a balanced double-entry ledger pair.

## API contract

### `POST /wallets`

Creates a wallet. Needed so transfers can run against known accounts.

```json
{ "id": "wallet_1", "balance": 1000 }
```

- `id` optional; generated UUID if omitted.
- `balance` optional; defaults to `0`. Integer minor units (example amount `100` is 100 units, not a float).

`201` with `{ "id", "balance" }`.

### `GET /wallets/{id}`

`200` with `{ "id", "balance" }`. `404` if missing.

### `POST /transfers`

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

Success (`201`):

```json
{
  "id": "...",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100,
  "status": "PROCESSED"
}
```

Replay of the same key and same payload returns the **same transfer body and same HTTP status**.

| Condition | HTTP | Side effects |
| --- | --- | --- |
| Validation (empty key, amount ≤ 0, same wallet) | 400 | none |
| Wallet missing | 404 | none; idempotency key not consumed |
| Same key, **different** payload | 409 | none |
| Insufficient funds | 422 | transfer `FAILED`, no ledger, no balance change; key consumed |
| Success | 201 | `PROCESSED`, two ledger rows, balances updated |

`GET /healthz` → `200`.

## Schema

- **wallets**: `id` PK, `balance BIGINT CHECK (balance >= 0)`. Stored balance is the source of truth for authorization; ledger is the audit trail.
- **transfers**: `id` PK, `from_wallet_id`, `to_wallet_id` FKs, `amount > 0`, `status IN (PENDING, PROCESSED, FAILED)`, optional `failure_reason`.
- **ledger_entries**: FK to transfer and wallet, `entry_type IN (DEBIT, CREDIT)`, `amount > 0`, `UNIQUE (transfer_id, entry_type)` so a transfer cannot get two debits or two credits.
- **idempotency_records**: `idempotency_key` PK, `request_hash`, `status IN (IN_PROGRESS, COMPLETED)`, `transfer_id` FK, `http_status`.

Indexes: wallet FKs on transfers; `ledger_entries.transfer_id`.

## Side effects of a successful transfer

1. Claim idempotency key (`IN_PROGRESS`) in the same DB transaction.
2. Lock both wallets `SELECT ... FOR UPDATE` in lexicographic id order (deadlock avoidance).
3. Insert transfer `PENDING`.
4. If funds sufficient: debit/credit wallets, insert DEBIT + CREDIT ledger rows, set `PROCESSED`.
5. If not: set `FAILED`, no ledger, no balance change.
6. Mark idempotency `COMPLETED` with `transfer_id` and HTTP status.
7. Commit.

## Failure modes

- Request never reaches commit: transaction rolls back, including the idempotency insert; client may retry with the same key.
- Commit succeeds, response lost: retry hits unique key, loads completed transfer, returns original result.
- Insufficient funds: durable `FAILED` result bound to the key so retries do not later succeed if the wallet is topped up (explicit choice: the first decision is sticky).

## Idempotency

- Key stored in `idempotency_records` with a **primary key / unique constraint**.
- Request fingerprint: SHA-256 of `fromWalletId|toWalletId|amount`. Same key + same fingerprint → original transfer. Same key + different fingerprint → `409`.
- Duplicate detection is the unique insert. Concurrent duplicates: the second session waits on the unique index (Postgres), then `SELECT ... FOR UPDATE` and reads the completed row. No second transfer, ledger, or debit.
- Side effects run only in the transaction that successfully inserted `IN_PROGRESS`. `IN_PROGRESS` is never committed; a crash/rollback frees the key.

## Retry and consistency

- Single transaction boundary for claim + locks + ledger + balances + completion.
- Isolation: default `READ COMMITTED` plus row locks. Balance `CHECK (balance >= 0)` and conditional `UPDATE ... WHERE balance >= amount` as belt-and-suspenders.
- State machine: `PENDING → PROCESSED` or `PENDING → FAILED` only, in that same transaction (callers never see `PENDING` after commit).

## Concurrency (double spend)

Two transfers debiting the same wallet: both take `FOR UPDATE` on that wallet row; one waits. The second sees the updated balance. No application-only read-then-write.

## Observability

Structured logs (`slog`) for transfer id, wallets, amount, status, and idempotency hit vs miss. No metrics required.

## Testing strategy

Behavioral tests against PostgreSQL (embedded Postgres in `go test`, or `TEST_DATABASE_URL`):

- Happy path: balances, two balanced ledger rows, `PROCESSED`.
- Idempotent replay and concurrent same key.
- Concurrent distinct transfers from one wallet (no overdraft).
- Insufficient funds → `FAILED`, empty ledger for that transfer.
- Validation and key/payload conflict.

## Tradeoffs

- Stored balance vs ledger-only: stored balance + locks is simpler for authorization; ledger remains append-only proof. Rebuilding balance from the ledger is possible but not used at request time.
- Failed transfers consume the idempotency key so a later deposit cannot silently turn a retried failure into a success.
- Extra `POST /wallets` / `GET /wallets/{id}` beyond the assignment’s single required endpoint, so the service is exercisable without SQL.

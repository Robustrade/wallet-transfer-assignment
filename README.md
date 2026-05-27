# Wallet Transfer Service

This is a small Go service for wallet-to-wallet transfers. It focuses on the core correctness properties from the assessment: durable idempotency, atomic balance updates, double-entry ledger rows, safe transfer state transitions, and concurrent debit safety.

## Run

Requirements:

- Go 1.24+
- CGO-capable build environment for `github.com/mattn/go-sqlite3`

Commands:

```sh
make test
make lint
make fmt
make run
```

The service listens on `:8080` by default and stores data in `wallets.db`. Override with:

```sh
ADDR=:9090 WALLET_DB_MAX_OPEN_CONNS=8 WALLET_DB_DSN='file:wallets.db?_foreign_keys=on&_busy_timeout=5000' make run
```

## API

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

Successful response returns `201 Created` with the transfer, balances after the mutation, and the two ledger entries. Insufficient funds returns `422 Unprocessable Entity` with a `FAILED` transfer. Reusing an idempotency key with different transfer parameters returns `409 Conflict`.

Wallet creation is intentionally kept out of the public API for this assessment. Tests seed wallets through the repository so the transfer path stays focused.

## Design

The implementation is layered:

- `internal/httpapi`: HTTP request/response mapping and validation.
- `internal/service`: transfer workflow, idempotency behavior, state transitions, and business decisions.
- `internal/store`: SQLite schema, transactions, and persistence operations.
- `internal/domain`: transfer, ledger, and response models.

The schema contains:

- `wallets`: stored balances with `CHECK (balance >= 0)`.
- `transfers`: one row per attempted transfer that reaches wallet validation, with `PENDING`, `PROCESSED`, or `FAILED` state.
- `ledger_entries`: exactly one `DEBIT` and one `CREDIT` row per processed transfer, enforced by `UNIQUE (transfer_id, type)`.
- `idempotency_records`: explicit `idempotency_key`, durable request hash, and stored original response for replay.

## Consistency Strategy

Each request is handled inside one database transaction. The idempotency row is inserted first under a unique key; duplicate requests load the stored original response instead of re-running side effects. The request hash excludes the idempotency key and covers the source wallet, destination wallet, and amount, so accidental key reuse with different parameters is rejected.

Balances are stored on `wallets`. Debits use a guarded update:

```sql
UPDATE wallets
SET balance = balance - ?
WHERE id = ?
  AND balance >= ?
```

That makes the database enforce no-overdraft behavior. SQLite is configured with WAL mode, a busy timeout, and a configurable connection pool (`WALLET_DB_MAX_OPEN_CONNS`, default `4`) so requests contend at the database layer while preserving correctness. On PostgreSQL, the equivalent production approach would use row-level locks or the same conditional update inside a transaction.

Processed transfers atomically update both balances, insert the debit and credit ledger rows, and transition from `PENDING` to `PROCESSED`. Insufficient-funds transfers transition from `PENDING` to `FAILED` and produce no ledger entries.

## Testing

The service tests cover:

- successful transfer execution
- idempotent replay with no duplicate side effects
- idempotency key conflict detection
- insufficient funds failure behavior
- double-entry ledger balancing
- concurrent debits that would otherwise double-spend the same wallet

## AI Use Note

I used Cursor as an AI coding assistant in the same way I would use AI in a professional engineering workflow: to accelerate repository exploration, compare design options, draft implementation scaffolding, generate focused test cases, and review documentation for completeness. I kept the system design and correctness decisions under human review, especially around transaction boundaries, idempotency semantics, ledger invariants, and concurrency behavior.

I used AI iteratively rather than as a one-shot generator: inspect requirements, propose an implementation plan, implement small slices, run verification, review failures, and refine the solution until tests and lint passed. The AI assistance was limited to development support; the final design tradeoffs, code review, and acceptance criteria were evaluated against the assignment rubric.

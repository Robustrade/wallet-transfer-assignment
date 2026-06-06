# Repository Layer Design

## Problem Statement

The repository layer is the only layer that communicates with the database.
It exposes focused interfaces so the service layer can compose multiple
operations into a single atomic transaction without knowing any SQL.

## Expected Behaviour

Each repository method does exactly one thing: insert, select, or update a
single entity type. Business rules (balance validation, state machine) live
in the service layer only.

## Persistence Contract

### Querier interface

Both `*pgxpool.Pool` and `pgx.Tx` satisfy the `Querier` interface, allowing
all repositories to be called either inside or outside a transaction:

```go
type Querier interface {
    QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
    Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}
```

Methods that must participate in a transaction accept `pgx.Tx` explicitly
(e.g. `GetByIDForUpdate`, `UpdateBalance`, `UpdateStatus`, `CreateEntries`).
Read-only methods accept the broader `Querier` so they can be used with the
pool directly.

### WalletRepository

| Method | Description |
|---|---|
| `Create(ctx, q, Wallet)` | Inserts a new wallet row. |
| `GetByID(ctx, q, id)` | Reads a wallet; returns `ErrWalletNotFound` if absent. |
| `GetByIDForUpdate(ctx, tx, id)` | Reads + row-locks a wallet within a transaction. |
| `UpdateBalance(ctx, tx, id, amount)` | Sets balance and increments `version`. |

### TransferRepository

| Method | Description |
|---|---|
| `Create(ctx, q, Transfer)` | Inserts transfer in PENDING status. |
| `GetByID(ctx, q, id)` | Reads a transfer; returns `ErrTransferNotFound` if absent. |
| `UpdateStatus(ctx, tx, id, status)` | Transitions status; returns `ErrTransferNotFound` if row gone. |

### LedgerRepository

| Method | Description |
|---|---|
| `CreateEntries(ctx, tx, debit, credit)` | Inserts both ledger entries sequentially within one tx. |

### IdempotencyRepository

| Method | Description |
|---|---|
| `Get(ctx, q, key)` | Returns cached record or `ErrTransferNotFound` if not yet stored. |
| `Create(ctx, tx, record)` | Stores the API response atomically inside the transfer tx. |

## Query Strategy

- **Pessimistic locking:** `SELECT … FOR UPDATE` is used on both wallet rows
  inside the transfer transaction. Locks are acquired in ascending UUID byte
  order (enforced in the service layer) to prevent deadlocks.
- **Integer amounts:** `balance` and `amount` columns are `BIGINT` (minor
  units / cents). The repository converts `domain.Amount` ↔ `int64` at the
  boundary — no float handling anywhere.
- **Integer status/type:** `status` and `type` columns are `SMALLINT`. The
  repository converts `domain.TransferStatus` ↔ `int` at the boundary.

## Side Effects

All writes produce log entries via `slog` with `"layer": "repo"` and relevant
entity IDs before returning errors upward.

## Failure Modes

| Error | Origin |
|---|---|
| `domain.ErrWalletNotFound` | `pgx.ErrNoRows` on wallet queries |
| `domain.ErrTransferNotFound` | `pgx.ErrNoRows` on transfer/idempotency queries |
| Wrapped `fmt.Errorf("repo: …: %w", err)` | Any other DB error |

Callers use `errors.Is` to distinguish domain sentinel errors from
infrastructure failures.

## Idempotency Behaviour

The `IdempotencyRepository.Create` is called within the same transaction as the
balance updates and ledger writes. If the transaction rolls back, the idempotency
record is never committed — preventing a scenario where a failed transfer is
reported as successful on retry.

## Testing Strategy

Integration tests in `internal/repository/repository_integration_test.go` use
`testcontainers-go` to spin up a real PostgreSQL 16 container per test, apply
migrations from the embedded FS, and verify all CRUD operations against a live
database. Tests are skipped with `-short` for fast unit test runs.

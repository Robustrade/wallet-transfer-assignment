# Wallet Transfer Service

A wallet-to-wallet transfer service with idempotency, double-entry ledger, and concurrency-safe balance tracking.

## Requirements

- Go 1.24+
- GCC (for `mattn/go-sqlite3` CGO build)
  - Linux: `sudo apt-get install gcc`
  - macOS: `xcode-select --install`
  - Windows: install [MSYS2](https://www.msys2.org/) and add `C:\msys64\ucrt64\bin` to PATH

## Run

```bash
go build ./cmd/server
./server
```

By default the server listens on `:8080` and creates `wallet.db` in the working directory.

**Environment variables:**

| Variable       | Default                          | Description                  |
|----------------|----------------------------------|------------------------------|
| `PORT`         | `8080`                           | HTTP listen port             |
| `DATABASE_DSN` | `file:wallet.db?_foreign_keys=on`| SQLite DSN                   |

## API

### Create a wallet

```
POST /wallets
```

```json
{ "initialBalance": 1000 }
```

### Get wallet balance

```
GET /wallets/{id}
```

### Create a transfer

```
POST /transfers
```

```json
{
  "idempotencyKey": "unique-client-key",
  "fromWalletId": "<wallet-id>",
  "toWalletId": "<wallet-id>",
  "amount": 100
}
```

- `idempotencyKey` is optional. When provided, duplicate requests return the original result without re-executing the transfer.
- `amount` is an integer (smallest currency unit, e.g. cents).

**Responses:**

| Status | Meaning                                      |
|--------|----------------------------------------------|
| 201    | Transfer created and processed               |
| 200    | Idempotent replay — original result returned |
| 400    | Validation error                             |
| 404    | Wallet not found                             |
| 422    | Insufficient funds (transfer saved as FAILED)|
| 500    | Internal server error                        |

### Get a transfer

```
GET /transfers/{id}
```

## Test

```bash
CGO_ENABLED=1 go test ./... -race -cover
```

## Lint & format check

```bash
golangci-lint run ./...
gofmt -l .
```

Or via Make:

```bash
make test
make lint
make fmt-check
```

## Design notes

### Database schema

Four tables: `wallets`, `transfers`, `ledger_entries`. Key constraints:

- `CHECK (balance >= 0)` — database-level guard against negative balances
- `UNIQUE (idempotency_key)` on `transfers` — deduplication enforced at the DB level, not just application level
- `CHECK (from_wallet_id != to_wallet_id)` — self-transfers rejected at schema level
- `CHECK (amount > 0)` and `CHECK (status IN (...))` — invalid states cannot be persisted
- Foreign keys enabled (`PRAGMA foreign_keys = ON`) — referential integrity enforced

### Idempotency strategy

The `idempotency_key` column has a `UNIQUE` constraint. On each request:

1. Look up the key before opening a transaction (fast path for replays).
2. Inside the transaction, `INSERT` the transfer record. If the insert fails with a unique constraint error, another concurrent request with the same key already committed — fetch and return that record.
3. This means deduplication is enforced at the database level even under concurrent requests.

A FAILED transfer is persisted so that replays of a failed request return the same FAILED result rather than retrying the transfer.

### Concurrency strategy

SQLite does not support row-level locking (`SELECT FOR UPDATE`). The connection pool is limited to a single open connection (`db.SetMaxOpenConns(1)`), which serialises all writes through the driver. No two transactions can modify wallet balances simultaneously.

For a PostgreSQL deployment the right approach is `SELECT ... FOR UPDATE` on both wallet rows (locked in a consistent ID order to prevent deadlocks), which allows higher write throughput while keeping the same correctness guarantees.

### Transfer state machine

```
PENDING → PROCESSED   (sufficient funds, all steps committed)
PENDING → FAILED      (insufficient funds, persisted so idempotent replays are stable)
```

State transitions happen inside a single database transaction. A record visible in the database is always either PROCESSED or FAILED — PENDING records left by a crashed process are rolled back automatically by SQLite's WAL on next startup.

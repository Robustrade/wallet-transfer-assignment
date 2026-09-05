# Wallet Transfer Service

Go and Gin service for atomic wallet-to-wallet transfers backed by PostgreSQL.
The code is organized by feature under `internal/`: wallet, transfer, ledger,
and idempotency. Each application service depends on an interface, while
PostgreSQL is provided by the infrastructure adapters.

## Run

```sh
docker compose up -d postgres
go run ./cmd/server
```

## API workflow

Create a wallet with `POST /wallets`:

```json
{
  "name": "Alice Wallet",
  "initialBalance": 1000
}
```

The response contains the generated wallet `id`. Create a second wallet with `initialBalance: 0`, then create a transfer with `POST /transfers`:

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

Seed wallets directly after starting PostgreSQL:

```sql
INSERT INTO wallets (id, balance, currency) VALUES ('00000000-0000-0000-0000-000000000001', 1000, 'USD'), ('00000000-0000-0000-0000-000000000002', 0, 'USD');
```

Read a wallet balance with `GET /wallets/{walletId}`. Read transactions involving one wallet with `GET /wallets/{walletId}/transfers`, or read all transactions with `GET /transfers`.

Postman files are in `postman/`. Import the complete collection and local environment, select the `Wallet Transfer Local` environment, and run wallet creation before the transfer.

## Endpoints

- `POST /wallets` creates a wallet with `name`, `initialBalance`, and optional `currency`.
- `GET /wallets` lists all wallets.
- `GET /wallets/{walletId}` returns the current balance.
- `PATCH /wallets/{walletId}/balance` updates a wallet balance with `{ "balance": 1500 }`.
- `GET /wallets/{walletId}/transfers` returns that wallet's transactions.
- `POST /transfers` creates an idempotent wallet transfer.
- `GET /transfers` returns all transactions, newest first.

Versioned API routes are under `/api/v1`: `POST /api/v1/wallets`, `GET /api/v1/wallets`, `GET /api/v1/wallets/{walletId}`, `POST /api/v1/wallets/{walletId}/balance`, `GET /api/v1/wallets/{walletId}/balance`, `GET /api/v1/wallets/{walletId}/balance/history`, `GET /api/v1/wallets/{walletId}/transactions`, `POST /api/v1/transfers`, and `GET /api/v1/transfers/{transferId}`. Health is available at `/health`.

If an existing database reports a missing `request_hash` column, apply migration `migrations/000008_complete_idempotency_columns.up.sql` before starting the service.

The repository stores the idempotency key and request fingerprint in the same transaction as the transfer. Duplicate requests return the stored transfer, while reusing a key with different transfer details returns `409 Conflict`. Wallet rows are locked in deterministic ID order inside the transaction, preventing concurrent debits from overspending. A successful transfer updates both balances and writes exactly one debit and one credit ledger entry before commit.

## Test

```sh
go test ./...
go vet ./...
go test -race ./...
```

The assignment-specific documentation is in `docs/architecture.md`, `docs/transaction-flow.md`, and `docs/ai-usage.md`. Run `make migrate-up` against a clean PostgreSQL database before integration or concurrency tests.
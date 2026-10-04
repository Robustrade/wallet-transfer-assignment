# Wallet Transfer Service

A small Go service for wallet-to-wallet transfers. A transfer is idempotent, atomic, and recorded as a double-entry ledger posting.

Detailed design lives in [`docs/`](docs/architecture.md).

## What it guarantees

- `POST /transfers` with the same `idempotencyKey` and the same wallets and amount returns the original transfer and does not move money again.
- The same key with a different payload is rejected.
- Each processed transfer writes one debit and one credit for the same amount.
- Concurrent debits of one wallet cannot drive the balance negative or lose an update.
- Amounts are integers. The service does not use floating point for money.

## Quick start

PostgreSQL 15+ and Go 1.25+ are required for a local process. Docker Compose is the other path.

```bash
export WALLET_DB_PASSWORD=wallet
createdb -U wallet wallet 2>/dev/null || true
make run
```

`config.yaml` has no password. The process exits if `WALLET_DB_PASSWORD` and `DATABASE_URL` are both unset.

With Docker:

```bash
docker compose up --build
```

Compose starts PostgreSQL and the API. The local database password in Compose is `wallet` and is only for this development stack.

## Try a transfer

```bash
curl -s -X POST localhost:8080/wallets -H 'content-type: application/json' \
  -d '{"id":"wallet_1","initialBalance":500}'

curl -s -X POST localhost:8080/wallets -H 'content-type: application/json' \
  -d '{"id":"wallet_2","initialBalance":0}'

curl -s -X POST localhost:8080/transfers -H 'content-type: application/json' \
  -d '{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
```

Sending that transfer request again returns the same transfer id and does not debit `wallet_1` a second time.

## Tests and checks

```bash
make test
make test-race
make vet
make cover
```

Integration tests start a temporary PostgreSQL cluster when `initdb` is on `PATH` or under Homebrew. Set `WALLET_TEST_DATABASE_URL` to use a server you already have, or `WALLET_POSTGRES_BIN` to point at a `bin` directory that contains `initdb` and `pg_ctl`.

`go test -short ./...` skips the database tests.

Migrations run on startup. `make migrate` applies them and exits.

## Layout

```text
cmd/wallet-service          composition root, HTTP server, shutdown
internal/domain             wallets, transfers, ledger, state machine
internal/app                use cases and ports
internal/adapter/httpapi    REST handlers
internal/adapter/postgres   SQL, locks, migrations
internal/platform           config, logging, ids
docs                        architecture, API, schema, ADRs
```

## Assumptions that affect the API

- `idempotencyKey` is required.
- Amounts are minor units of one implicit currency, from 1 to 1_000_000_000_000_000.
- Opening a wallet with `initialBalance` is administrative funding. It is not a ledger transfer. Reconciliation adds that opening amount to the ledger net.
- A missing wallet does not consume the idempotency key. Insufficient funds does, and the stored result is `FAILED`.
- There is no authentication. Idempotency keys are global.

See [docs/decisions](docs/decisions/0002-locking-and-transactions.md) for the lock and transaction strategy.

# Wallet service

Requires Go 1.25.11+ and Docker Compose. Exposes `POST /transfers` for atomic,
idempotent transfers and `GET /healthz` for HTTP liveness.

## Run

From `wallet-service`:

```sh
cp .env.example .env
set -a
. ./.env
set +a
make db-up
make migrate-up
make seed
make run
```

The shell commands export `.env`; the application does not load it automatically.
`DATABASE_URL` is required and checked at startup (five-second timeout).
`HTTP_ADDR` defaults to `:8080`. Ctrl+C/SIGTERM allows five seconds for shutdown.
Seeds create INR wallets `wallet_1=500` and `wallet_2=200` minor units and never
reset existing balances. `make seed` targets the local Compose database.

```sh
curl -i http://localhost:8080/healthz
```

Expected: HTTP 200, `Content-Type: application/json`, and `{"status":"ok"}`.
This endpoint checks HTTP liveness; it does not query the database.
`make db-down` stops PostgreSQL while retaining its volume.

## Transfers

With fresh seeds, this returns 201 and changes balances from 500/200 to 400/300:

```sh
curl -i http://localhost:8080/transfers -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"demo-success","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
```

Repeat unchanged to replay the exact status/body without moving money again.
Insufficient funds returns 422 and saves a `FAILED` transfer without ledger entries:

```sh
curl -i http://localhost:8080/transfers -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"demo-failure","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":1000}'
```

Amounts are integer minor units; JSON bodies are limited to 4 KiB. Transfer responses
include `X-Request-ID` for logs. Retry network/503 failures with the same key/payload
and bounded backoff. Saved business failures replay unchanged; a new attempt needs
a new key. See the [error contract](docs/design.md#api) for other responses.

## Checks

With the environment above exported:

```sh
make check
```

Runs formatting checks, vet, build, and `go test -race ./...`. Use `gofmt -w .`
to fix formatting. `TEST_DATABASE_URL` must point to PostgreSQL with a role allowed
to create databases; tests create/drop isolated databases without modifying the
supplied database. Plain `go test ./...` skips database tests when that variable
is absent; `make test` and CI require it.

Service tests cover atomic transfers, replay/conflicts, saved business failures,
concurrent debits, rollback/retry, cancellation, and simulated lost commit acknowledgments.

Migrations use pinned golang-migrate and track versions in `schema_migrations`.
`make migrate-down` removes the schema and data; use only on disposable databases.
After a dirty migration, inspect and repair the cause before correcting its version;
never automatically force or retry it. See the [migration tool](https://github.com/golang-migrate/migrate).

See the [plan](IMPLEMENTATION_PLAN.md), [design contract](docs/design.md), and
[migration notes](migrations/README.md).

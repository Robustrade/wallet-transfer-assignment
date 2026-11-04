# Run the wallet transfer API locally

## Prerequisites

- Go 1.23 or newer
- Docker with the Compose plugin, or a reachable PostgreSQL instance

## Start PostgreSQL

From the repository root:

```sh
docker compose up -d --wait postgres
```

The Compose service creates a local database named `wallet_transfer` with user `wallet` and development password `wallet_dev`.

## Configure the application

Copy the example environment file:

```sh
cp .env.example .env
set -a
. ./.env
set +a
```

The application reads environment variables directly; it does not load `.env` automatically. The commands above export the values into the current shell.

## Start the server

```sh
go run ./cmd/server
```

At startup the application:
1. Loads and validates the PostgreSQL connection pool configuration.
2. Opens and pings PostgreSQL using the pgx `database/sql` driver.
3. Applies pending embedded SQL migrations under a PostgreSQL advisory lock.
4. Constructs the transfer service and registers the HTTP handler.
5. Starts listening on `HTTP_ADDR` (default `:8080`).

The server exposes `GET /healthz` as a database readiness check and `POST /transfers` for transfers.

## Send a transfer

Create two wallets first. For example, using `psql`:

```sh
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 <<'SQL'
INSERT INTO wallets (id, currency, balance)
VALUES ('wallet_1', 'INR', 100000), ('wallet_2', 'INR', 5000);
SQL
```

Then call the API:

```sh
curl -i http://localhost:8080/transfers \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: transfer-demo-001' \
  -d '{"fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":1000}'
```

The amount is expressed in the smallest currency unit (for INR, paise). Repeating the same request with the same idempotency key returns the original transfer. Reusing the key with a different payload returns HTTP 409.

## Run unit and integration tests

Unit tests:

```sh
go test ./...
go test -race -cover ./...
go vet ./...
```

The PostgreSQL integration tests are opt-in and use a temporary isolated schema. Set `TEST_DATABASE_URL` to a disposable test database URL before running:

```sh
TEST_DATABASE_URL="$DATABASE_URL" go test -v ./internal/integration
```

The integration tests create and drop a uniquely named schema. Do not point `TEST_DATABASE_URL` at a database where the test role cannot create schemas. The test role also needs permission to create and drop objects in that database.

## Shutdown

Send SIGINT (Ctrl+C) or SIGTERM. The server stops accepting new connections and gives in-flight requests up to 10 seconds to finish before closing.

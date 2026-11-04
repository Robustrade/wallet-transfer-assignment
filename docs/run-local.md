# Run the wallet transfer API locally

## Prerequisites

- Go 1.23 or newer
- PostgreSQL installed and running locally, or a reachable PostgreSQL instance
- The PostgreSQL database and application role created

## Configure the application

Set `DATABASE_URL` to the connection URL for your existing PostgreSQL database. For example:

```sh
export DATABASE_URL='postgres://<user>:<password>@localhost:5432/<database>?sslmode=disable'
export HTTP_ADDR=':8080'
```

Replace the placeholders with the PostgreSQL role, password, and database you already created. The application reads environment variables directly; it does not load a `.env` file automatically.

Optional connection-pool settings:

```sh
export DB_MAX_OPEN_CONNS=25
export DB_MAX_IDLE_CONNS=5
export DB_CONN_MAX_LIFETIME=5m
```

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

**Existing schema note:** The startup migration runner records applied migration versions in `schema_migrations`. If you created the application tables manually rather than by applying the repository migration, do not start the server until the existing schema has been reconciled with the migration history. The initial migration creates the `wallets`, `transfers`, `ledger_entries`, and `idempotency_records` tables.

## Send a transfer

Create two wallets first if they do not already exist. For example, using `psql`:

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

The integration tests create and drop a uniquely named schema. The test role needs permission to create and drop schemas and objects in that database. Use a disposable test database, not a production database.

## Shutdown

Send SIGINT (Ctrl+C) or SIGTERM. The server stops accepting new connections and gives in-flight requests up to 10 seconds to finish before closing.

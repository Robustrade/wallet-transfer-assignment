# Wallet Transfer Service

A high-concurrency, transactional wallet transfer service in Go and PostgreSQL with atomic double-entry ledger bookkeeping, deterministic row locking, and distributed idempotency replay. For architectural rationale, database constraints, and concurrency strategies, see [`docs/design.md`](./docs/design.md).

---

## Tech Stack

- **Language**: Go 1.24
- **Database**: PostgreSQL 18 (Docker container)
- **Database Driver / Pool**: `jackc/pgx/v5`
- **Database Migrations**: `pressly/goose/v3`
- **SQL Queries**: `sqlc` (type-safe SQL compiler)
- **HTTP Routing**: `go-chi/chi/v5`
- **API Specification**: OpenAPI 3.0.3 ([`openapi.yaml`](./openapi.yaml))

---

## How to Run

### Prerequisites

- [Go 1.24+](https://go.dev/dl/)
- [Docker & Docker Compose](https://docs.docker.com/get-docker/)
- `make` (standard on Linux/macOS)

### 1. Install Development Tools (Setup)

Check and automatically install required CLI tools (`goose` for migrations, `sqlc`, `mockgen`) if not already present:

```bash
make setup
```

### 2. Start Local PostgreSQL

Start the isolated PostgreSQL 18 development container on port `5433`:

```bash
make dev-up
```

### 3. Apply Database Migrations

Apply database schemas (`wallets`, `transfers`, `ledger_entries`, `idempotency_records`):

```bash
make migrate-up
```

### 4. Start the API Server

Run the HTTP server on port `8080`:

```bash
make run
```

*Alternatively, compile the binary and run directly:*

```bash
make build
./bin/api
```

The API server will connect to PostgreSQL and listen on `http://localhost:8080`.

---

## Running Tests

All test suites run against the live PostgreSQL database:

```bash
# Run unit and integration tests
make test

# Run tests with Go data race detector
go test -race -v -count=1 ./test/...
```

---

## API Examples (`curl`)

All monetary values are stored and transferred as 64-bit integers in minor currency units (paise: ₹1.00 = 100 paise). See [`openapi.yaml`](./openapi.yaml) for full schema details.

### 1. Create / Seed Wallets

```bash
# Seed Alice with 10,000 paise (₹100)
curl -s -X POST http://localhost:8080/wallets \
  -H "Content-Type: application/json" \
  -d '{"id": "alice", "balance": 10000}' | jq

# Seed Bob with 2,000 paise (₹20)
curl -s -X POST http://localhost:8080/wallets \
  -H "Content-Type: application/json" \
  -d '{"id": "bob", "balance": 2000}' | jq
```

### 2. Query Wallet Balance

```bash
curl -s http://localhost:8080/wallets/alice | jq
```

### 3. Execute an Idempotent Transfer (Alice -> Bob)

```bash
curl -s -i -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "tx-001",
    "fromWalletId": "alice",
    "toWalletId": "bob",
    "amount": 3000
  }'
```

*Returns `201 Created` on the initial execution. Re-running the exact request returns `200 OK` with header `X-Idempotent-Replay: true` without double-debiting.*

### 4. Fetch Wallet Transfer History

```bash
curl -s "http://localhost:8080/wallets/alice/transfers?limit=10&offset=0" | jq
```

---

## Teardown

To stop and remove the local PostgreSQL container and database volumes:

```bash
make dev-down
```

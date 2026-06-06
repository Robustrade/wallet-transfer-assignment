# Wallet Transfer Assignment Repository

> Branch: `solution/waseem`

## Prerequisites

- Go 1.25+
- Docker Desktop

## Quick start

```bash
# Start PostgreSQL + App
make up
```

## Development commands

| Command | Description |
|---------|-------------|
| `make up` | Start PostgreSQL + App containers via Docker Compose |
| `make down` | Stop and remove containers |
| `make build` | Compile the server binary |
| `make build-dev` | Compile the application binary for tests |
| `make fmt` | Run `gofmt` (NOTE: File-system dependent) |
| `make lint` | Run `golangci-lint` in "dev" container |
| `make test` | Run all tests (unit + functional) |
| `make test-coverage` | Convert raw code coverage output to HTML for readability |
| `make test-unit` | Run only unit tests (w/o test-containers) |

## Running tests

### Quick

```bash
# All tests with race detector and coverage (inside Docker dev container)
make test
```

### Manual

From inside `dev` container

```bash
docker compose run --rm --no-deps dev

# Unit tests only (domain, service, handler — no Docker)
go test ./internal/domain/... ./internal/handler/... ./internal/service/... -count=1

# Integration tests (requires Docker)
go test ./internal/repository/... ./internal/service/... -count=1 -run Integration
```

## API

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/wallets` | Create a new wallet (zero balance) |
| `POST` | `/magic` | Deposit a random amount (100–10 000 cents) from the nature wallet |
| `GET` | `/wallets/{id}` | Fetch a wallet and its current balance |
| `POST` | `/transfers` | Initiate a transfer (idempotent via `idempotencyKey`) |
| `GET` | `/transfers/{id}` | Fetch a transfer by UUID |

### POST /wallets

No request body. Creates a new wallet with a zero balance and a freshly
generated UUID v7.

- Returns `201 Created` with the new wallet ID and balance.
- Successful response:
  ```json
  {
    "id": "<uuid>",
    "balance": 0
  }
  ```

### POST /magic

Deposits a randomly chosen amount between **100 cents ($1.00)** and **10 000 cents ($100.00)** from the system *nature* wallet into the specified destination wallet.

```json
{
  "idempotencyKey": "unique-client-key",
  "toWalletId": "<uuid>"
}
```

- No `fromWalletId` or `amount` fields — the server fills those in.
- Returns `201 Created` for a new deposit, `200 OK` for idempotent replays.
- The same amount is always returned for a given `idempotencyKey`.
- Successful response (same shape as `/transfers`):
  ```json
  {
    "id": "<uuid>",
    "status": "PROCESSED",
    "amount": 4231
  }
  ```

### GET /wallets/{id}

Returns the current balance of a wallet.

- Returns `200 OK` with wallet ID and balance.
- Successful response:
  ```json
  {
    "id": "<uuid>",
    "balance": 50000
  }
  ```

### POST /transfers

```json
{
  "idempotencyKey": "unique-client-key",
  "fromWalletId": "<uuid>",
  "toWalletId": "<uuid>",
  "amount": 10000
}
```

- `amount` is in minor units (cents). `10000` = $100.00.
- Returns `201 Created` for new transfers, `200 OK` for idempotent replays.
- Successful response:
  ```json
  {
    "id": "<uuid>",
    "status": "PROCESSED",
    "amount": 10000
  }
  ```

### GET /transfers/{id}

Returns the current status of a transfer by UUID.

- Returns `200 OK` with transfer ID, status, and amount.
- Successful response:
  ```json
  {
    "id": "<uuid>",
    "status": "PROCESSED",
    "amount": 10000
  }
  ```

### Sample Requests

```bash
# 1) Create wallet (201 Created)
curl -i -X POST http://localhost:8080/wallets

# 2) Magic deposit — random amount from nature wallet (201 Created)
curl -i -X POST http://localhost:8080/magic \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "magic-demo-001",
    "toWalletId": "22222222-2222-2222-2222-222222222222"
  }'

# 2a) Replay magic deposit (same idempotencyKey: 200 OK, same amount)
curl -i -X POST http://localhost:8080/magic \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "magic-demo-001",
    "toWalletId": "22222222-2222-2222-2222-222222222222"
  }'

# 3) Get wallet balance
curl -i http://localhost:8080/wallets/<wallet-uuid>

# 4) Initiate transfer (first call: 201 Created)
curl -i -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "demo-key-001",
    "fromWalletId": "11111111-1111-1111-1111-111111111111",
    "toWalletId": "22222222-2222-2222-2222-222222222222",
    "amount": 10000
  }'

# 4a) Replay same transfer (same idempotencyKey: 200 OK)
curl -i -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "demo-key-001",
    "fromWalletId": "11111111-1111-1111-1111-111111111111",
    "toWalletId": "22222222-2222-2222-2222-222222222222",
    "amount": 10000
  }'

# 5) Transfer status
curl -i http://localhost:8080/transfers/<transfer-uuid>
```

## Architecture

```
cmd/server/          — entrypoint, wiring, graceful shutdown
internal/domain/     — value types, entities, sentinel errors
internal/repository/ — PostgreSQL adapters (pgx v5)
internal/service/    — transfer workflow, idempotency, concurrency
internal/handler/    — HTTP handlers (chi router)
internal/db/         — connection pool, migration runner
migrations/          — SQL migrations (golang-migrate, embedded)
docs/                — design notes per layer
```

See `docs/` for per-layer design rationale, prompts history and 
future improvements plan.

# Wallet Transfer Service

A small Go service for **wallet-to-wallet transfers** with idempotent request handling, a double-entry ledger, and concurrency-safe balances.

- **Stack:** Go, Fiber v3, GORM, PostgreSQL, golang-migrate
- **Assignment:** [`ASSIGNMENT.md`](./ASSIGNMENT.md)

## What it guarantees

| Guarantee                                  | How                                                                                                                                      |
| ------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------- |
| One committed transfer per idempotency key | `idempotency_records` with a `UNIQUE` key; the same key replays the stored response                                                      |
| Same key, different payload is rejected    | Request fingerprint (SHA-256) compared on replay, returns `409`                                                                          |
| No double spend                            | Both wallets locked with `SELECT … FOR UPDATE`, always in ascending id order, **before** the transfer row is inserted                    |
| Balanced ledger                            | Exactly one `DEBIT` and one `CREDIT` per processed transfer (`UNIQUE (transfer_id, entry_type)`)                                         |
| Atomicity                                  | Balances, ledger, transfer status and the stored response commit in one transaction                                                      |
| Safe state machine                         | `PENDING → PROCESSED` or `PENDING → FAILED` only, via a guarded `WHERE status = 'PENDING'` update                                        |
| Retry safety                               | Deadlock (`40P01`) and serialization (`40001`) errors are retried with backoff; an uncertain commit is resolved from the stored response |

Details and reasoning are in [`docs/`](./docs).

## Documentation

| Document                                         | Contents                                           |
| ------------------------------------------------ | -------------------------------------------------- |
| [`docs/REQUIREMENTS.md`](./docs/REQUIREMENTS.md) | Functional and non-functional requirements         |
| [`docs/ARCHITECTURE.md`](./docs/ARCHITECTURE.md) | Components, layers, transaction flow               |
| [`docs/API.md`](./docs/API.md)                   | HTTP contract, status codes, idempotency behaviour |
| [`docs/DATABASE.md`](./docs/DATABASE.md)         | Schema, constraints, indexes                       |
| [`docs/DECISIONS.md`](./docs/DECISIONS.md)       | Architecture decision records and trade-offs       |
| [`docs/TESTING.md`](./docs/TESTING.md)           | Test strategy and scenarios                        |

## Project layout

```text
Dockerfile, docker-compose.yml    container image and one-command stack
docker/seed/                     seed SQL for the Docker stack
cmd/api/                         entry point (config, DB, HTTP server, graceful shutdown)
internal/
  bootstrap/                     dependency wiring
  config/  database/             environment config, PostgreSQL connection
  middleware/                    global HTTP middleware
  feature/wallet/                the wallet-transfer feature
    domain/                      entities, validation rules, state transitions
    model/                       GORM models (table mappings)
    repository/                  persistence, row locking, transaction manager
    service/                     transfer workflow, idempotency, retries
    handler/                     Fiber handler, request validation, error mapping
    route/                       route registration
    integration/                 PostgreSQL integration and concurrency tests
scripts/                         concurrency (race) test script
migrations/                      SQL migrations (wallets, transfers, ledger_entries, idempotency_records)
```

Dependencies point one way: `route → handler → service → repository → domain/model`. Each layer has its own `_test.go` files next to the code.

## Quick start with Docker

Only Docker (with Compose) is required. One command creates the database, applies migrations, seeds 100 wallets and starts the API:

```bash
make docker-up
```

(equivalent to `docker compose up -d --build --wait`)

| Service    | What it does                                                                                                                                                   |
| ---------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `postgres` | PostgreSQL 17; creates the `DB_NAME` database on first start (data in the `pgdata` volume)                                                                     |
| `migrate`  | Runs `./migrations` with golang-migrate, then exits                                                                                                            |
| `seed`     | Inserts 100 wallets with random balances (0–999,999) only if the table is empty ([`docker/seed/seed_wallets.sql`](./docker/seed/seed_wallets.sql)), then exits |
| `api`      | The Go service; starts after `migrate` and `seed` succeed                                                                                                      |

Then:

```bash
curl http://localhost:8090/health
```

```bash
curl -X POST http://localhost:8090/transfers -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"demo-key-0001","fromWalletId":1,"toWalletId":2,"amount":100}'
```

Other commands: `make docker-logs` (API logs), `make docker-down` (stop, keep data), `make docker-reset` (wipe the volume and rebuild a fresh database with new seed data).

Defaults can be overridden in a project-level `.env` or the shell: `DB_USER`, `DB_PASSWORD`, `DB_NAME` (defaults `wallet`/`wallet`/`wallet_db`), `API_HOST_PORT` (default `8090`) and `POSTGRES_HOST_PORT` (default `5433`, so it does not clash with a local PostgreSQL). 

## Getting started (without Docker)

### Prerequisites

- Go (see `go.mod`)
- PostgreSQL
- [`golang-migrate`](https://github.com/golang-migrate/migrate) CLI
- Optional: [`air`](https://github.com/air-verse/air) for live reload (`.air.toml` is included)

### Configure

```bash
cp .env.example .env
```

Edit `.env` for your database. Required variables: `APP_PORT`, `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`, `DB_SSLMODE`.

### Migrate and run

```bash
make migrate-up
```

```bash
go run ./cmd/api
```

Health check: `GET /health`.

## API

### `POST /transfers`

```json
{
  "idempotencyKey": "abc12345",
  "fromWalletId": 1,
  "toWalletId": 2,
  "amount": 100
}
```

- `idempotencyKey`: 8–64 characters (validated in the handler and domain, and enforced by the database).
- Wallet ids are positive integers and must differ. `amount` is a positive integer in minor units.

| Status | Meaning                                                                                                                                  |
| ------ | ---------------------------------------------------------------------------------------------------------------------------------------- |
| `201`  | Transfer processed: `{"status":"PROCESSED","transferId":<id>}`                                                                           |
| `409`  | Insufficient funds (recorded as `FAILED`), idempotency key reused with a different payload, or the original request is still in progress |
| `400`  | Invalid request                                                                                                                          |
| `404`  | Wallet not found                                                                                                                         |
| `503`  | Commit outcome uncertain: retry with the **same** idempotency key                                                                        |
| `500`  | Unexpected error                                                                                                                         |

Replaying the same key and payload returns the original status and a byte-identical body, including a recorded `409`. See [`docs/API.md`](./docs/API.md) for the full contract.

## Testing

Unit tests need no database: domain, handler, and service logic run against in-memory fakes that enforce the same constraints as the schema.

```bash
make test
```

Integration and concurrency tests run against the PostgreSQL database from `.env` (migrations must be applied). They create their own wallets and never touch existing rows.

```bash
make test-integration
```

They cover parallel duplicate keys, double-spend attempts, opposite-direction transfers (deadlock regression), money conservation, ledger invariants and schema constraints.

### Lint

Static analysis uses `golangci-lint` v1.64.8 (the version pinned in CI) with the rules in [`.golangci.yml`](./.golangci.yml): the default linters plus `errorlint`, `gocritic`, `goimports`, `gosec`, `noctx`, `whitespace` and a few others. The integration-test files are linted too.

```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8
```

```bash
make lint
```

### SonarQube Cloud

CI analyses every pull request with SonarQube (`.github/workflows/sonarqube.yml`, using [`sonar-project.properties`](./sonar-project.properties)). You can run the same analysis from your machine against [SonarQube Cloud](https://sonarcloud.io); only Docker is needed, because the SonarScanner CLI runs in a container.

1. In SonarQube Cloud import the repository, note your **organization key**, and make sure `sonar.projectKey` in `sonar-project.properties` matches the project key it assigns (usually `<organization>_<repository>`).
2. Create a token under **My Account → Security**.
3. Run the tests with coverage and upload the analysis:

```bash
SONAR_TOKEN=<your-token> SONAR_ORGANIZATION=<organization-key> make sonar-scan
```

This uploads the source code and coverage to sonarcloud.io; the results appear in the project dashboard. Commit your changes first, otherwise SonarQube reports missing blame information for the modified files.

Coverage comes from `make coverage` (unit tests, about 64%). To include the PostgreSQL integration tests, which exercise the repositories (about 90%), run `COVERAGE_TARGET=coverage-integration make sonar-scan` with the `.env` database available. Wiring code (`cmd/`, config, database, bootstrap, middleware, GORM models) is excluded from coverage.

### Manual race test

With the stack running (`make docker-up`), fire concurrent requests from Node.js 18+. Modes: `opposite` (A→B and B→A), `same-sender`, `random` and `same-key` (one idempotency key sent by every request).

```bash
MODE=opposite COUNT=100 node scripts/concurrency-test.js
```

```bash
MODE=same-key COUNT=100 node scripts/concurrency-test.js
```

The script targets `http://localhost:8090/transfers` by default; set `API_URL` (and `WALLET_A`/`WALLET_B`) to point elsewhere. See [`docs/TESTING.md`](./docs/TESTING.md) for the verification queries.

## Assumptions and trade-offs

- **The assignment's example request is illustrative.** It uses string wallet ids (`"wallet_1"`) and a short key (`"abc123"`); this service uses numeric wallet ids and idempotency keys of 8–64 characters. The equivalent request is `{"idempotencyKey": "abc12345", "fromWalletId": 1, "toWalletId": 2, "amount": 100}`. The assignment does not fix an id format; numeric ids come from database identity columns and keys of at least 8 characters avoid trivially guessable values.
- Balances are stored on `wallets` and updated under row locks (no balance derived from the ledger).
- Replay bodies are re-serialised to a canonical JSON form, because PostgreSQL `JSONB` reformats stored JSON.
- A key that is claimed but has no stored response yet returns `409` rather than re-executing the transfer.
- No authentication or idempotency-key scoping per client is implemented.
- The wallet-balance and transfer-history APIs are optional and not implemented.

## Repository template files

This repository started from an assignment template. Reviewer material is kept alongside the solution: [`evaluation_guide.md`](./evaluation_guide.md) and [`branch-protection-checklist.md`](./branch-protection-checklist.md).

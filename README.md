# Wallet Transfer Service

Wallet-to-wallet transfers with idempotent requests, a double-entry ledger and safe concurrent execution, built with Go, Gin, GORM and PostgreSQL.

Design, schema, idempotency and concurrency details: **[docs/DESIGN.md](docs/DESIGN.md)**.

## Requirements

- Go 1.23+
- Docker (runs Postgres for the app, and throwaway Postgres containers for the tests)

## Quick start

```bash
cp .env.example .env
make up     # Postgres on :5432, Adminer on :8080
make run    # API on :8000 (applies migrations on startup)
```

## Try it

```bash
curl -X POST localhost:8000/create-wallet -H 'Content-Type: application/json' \
  -d '{"walletId":"wallet_1","initialBalance":2000}'

curl -X POST localhost:8000/create-wallet -H 'Content-Type: application/json' \
  -d '{"walletId":"wallet_2","initialBalance":500}'

curl -i -X POST localhost:8000/transfers -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"demo-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":300}'
```

Send the same transfer again: you get the same body and status, plus the header `Idempotent-Replayed: true`, and no money moves a second time.

## API

| Endpoint | Success | Errors |
|---|---|---|
| `POST /create-wallet` | `201` wallet | `400` invalid input, `409` wallet already exists |
| `POST /transfers` | `201` transfer `PROCESSED` | `400` invalid input, `404` wallet not found, `409` idempotency key reused with a different request, `422` insufficient funds (transfer recorded as `FAILED`), `503` wallet busy (retry) |
| `GET /healthz` | `200` | `503` database unreachable |

Amounts are integers in minor units. Every response has an `X-Request-ID` header. Full contract: [DESIGN.md §3](docs/DESIGN.md#3-api-contract).

## How a transfer works

All of this happens in one database transaction, so it commits completely or not at all:

1. Claim the idempotency key (`INSERT … ON CONFLICT DO NOTHING`). If it was already used: same request → return the original transfer; different request → `409`.
2. Lock both wallets with `SELECT … FOR UPDATE`, always in sorted order (no double spending, no deadlocks).
3. Insert the transfer as `PENDING`.
4. Debit and credit the wallets, write one `DEBIT` and one `CREDIT` ledger entry, and mark the transfer `PROCESSED`, or mark it `FAILED` if funds are insufficient.

Database constraints back this up: balances can't go negative, one transfer per idempotency key, at most one debit and one credit per transfer. See [DESIGN.md §5–6](docs/DESIGN.md#5-schema).

## Tests

```bash
make test   # go test ./... -race -count=1 (Docker must be running)
```

- **Domain unit tests:** validation, state transitions, ledger entries, debit/credit rules.
- **API tests:** real HTTP requests against a real Postgres: wallet creation, transfers, insufficient funds, retries, idempotency key reuse, unknown wallets, invalid input.
- **Concurrency tests:** 20 simultaneous debits on one wallet (exactly the affordable number succeed), 20 simultaneous retries with one key (executes once), simultaneous A→B and B→A transfers (no deadlock).

Each test package starts its own throwaway Postgres with testcontainers. If the container can't start (for example, Docker isn't running), the tests fail with a clear message. To skip the database tests deliberately, run `SKIP_DB_TESTS=1 make test`.

## Lint and format

```bash
make lint        # golangci-lint v1.64.8 (same version as CI), run in Docker
make fmt-check   # fails if any file isn't gofmt-formatted
```

## Make targets

| Target | What it does |
|---|---|
| `make up` | start Postgres and Adminer |
| `make down` | stop them (data is kept in a Docker volume) |
| `make reset` | stop them and delete the data |
| `make run` | run the API |
| `make test` | run all tests with the race detector |
| `make lint` | run golangci-lint |
| `make fmt` / `make fmt-check` | format code / check formatting |

## Configuration

Settings come from environment variables, loaded from `.env` if present (see [.env.example](.env.example)).

| Variable | Example | Notes |
|---|---|---|
| `PORT` | `8000` | API port |
| `GIN_MODE` | `release` | `debug`, `release` or `test`; optional, defaults to `release` |
| `DB_HOST` / `DB_PORT` | `localhost` / `5432` | |
| `DB_USER` / `DB_PASSWORD` | `root` / `root` | also used by `docker-compose.yml` |
| `DB_NAME` | `wallet` | |
| `DB_SSLMODE` | `disable` | |

## Project structure

```text
cmd/server/            entry point: config, migrations, middleware, routes, graceful shutdown
internal/router/       all routes
internal/controller/   HTTP request/response handling
internal/response/     shared responses and error → status mapping
internal/middleware/   request IDs and request logging
internal/service/      business logic and transactions
internal/repository/   database access (GORM)
internal/domain/       entities, validation, state machine
internal/database/     connection and migration runner
internal/config/       environment configuration
internal/testutil/     test database helper
migrations/            versioned SQL schema
docs/DESIGN.md         design note
```

## Browsing the database

Adminer runs at <http://localhost:8080>. Log in with System **PostgreSQL**, Server **`postgres`**, user `root`, password `root`, database `wallet`.

---

## Original assignment notes

### Wallet Transfer Assignment Repository

This repository is a reusable coding assignment template for evaluating backend engineers on wallet transfers, idempotency, concurrency control, and double-entry ledger design.

#### Included

- `ASSIGNMENT.md` - candidate-facing prompt
- `.github/pull_request_template.md` - required PR structure
- `.github/workflows/ci.yml` - lint, format, test placeholder workflow
- `.github/workflows/sonarqube.yml` - SonarQube pull request analysis
- `.github/copilot-instructions.md` - repository-level Copilot review guidance
- `evaluation_guide.md` - reviewer rubric
- `branch-protection-checklist.md` - GitHub setup checklist

#### Intended use

1. Mark this repository as a GitHub template repository.
2. Create one private repository per candidate from the template.
3. Add the candidate as a collaborator.
4. Ask them to submit via a pull request into `main`.
5. Enable required checks, SonarQube, and Copilot review in GitHub.

#### Notes

- Copilot automatic pull request review is configured in GitHub repository or organization settings, not purely through files in the repo.
- The `copilot-instructions.md` file included here provides repository-specific review guidance once Copilot review is enabled.
- The CI workflow is language-agnostic by default and expects you to set the `LINT_CMD`, `FORMAT_CHECK_CMD`, and `TEST_CMD` repository variables or replace the commands directly.

#### How to Submit Assignment

1. **Fork this repository** to your own GitHub account.
2. Complete the assignment described in [`ASSIGNMENT.md`](./ASSIGNMENT.md).
3. **Raise a Pull Request** back to this repository (`main` branch) with your full solution.

Your PR branch should be named: `solution/<your-name>` (e.g., `solution/jane-doe`).

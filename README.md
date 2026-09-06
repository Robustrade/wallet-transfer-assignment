<<<<<<< HEAD
# Wallet Transfer Service

A production-quality wallet-to-wallet transfer service implementing idempotent,
atomic, double-entry ledger transfers with safe concurrent execution.

## Prerequisites

- Go 1.22+
- Docker + Docker Compose (or a local Postgres 16 instance)

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `DATABASE_URL` | `postgres://postgres:postgres@localhost:5432/wallet_transfer?sslmode=disable` | Postgres DSN |
| `MIGRATIONS_DIR` | `migrations` | Path to SQL migration files |
| `ADDR` | `:8080` | HTTP listen address |
| `TEST_DATABASE_URL` | `postgres://...wallet_transfer_test...` | DSN for the test database |

## Local Setup

```bash
# 1. Start Postgres
docker compose up -d

# 2. Run the server (migrations applied automatically on startup)
go run ./cmd/server/

# 3. Verify
curl http://localhost:8080/healthz
```

## Running Tests

```bash
# Unit tests (no database required)
go test ./internal/...

# Integration tests (requires a running Postgres)
go test -race -count=1 -timeout=60s ./tests/integration/...

# All tests with race detector
go test -race -count=1 -timeout=60s ./...
```

## Example Requests

### Create wallets

```bash
ALICE=$(curl -s -X POST http://localhost:8080/wallets \
  -H 'Content-Type: application/json' \
  -d '{"name":"Alice","initialBalance":1000}' | grep -o '"id":"[^"]*"' | cut -d'"' -f4)

BOB=$(curl -s -X POST http://localhost:8080/wallets \
  -H 'Content-Type: application/json' \
  -d '{"name":"Bob","initialBalance":500}' | grep -o '"id":"[^"]*"' | cut -d'"' -f4)
```

### Transfer funds

```bash
curl -X POST http://localhost:8080/transfers \
  -H 'Content-Type: application/json' \
  -d "{
    \"idempotencyKey\": \"txn-001\",
    \"fromWalletId\": \"$ALICE\",
    \"toWalletId\": \"$BOB\",
    \"amount\": 100
  }"
# → {"transferId":"...","status":"PROCESSED"}
```

### Idempotent retry (same result, no re-execution)

```bash
curl -X POST http://localhost:8080/transfers \
  -H 'Content-Type: application/json' \
  -d "{
    \"idempotencyKey\": \"txn-001\",
    \"fromWalletId\": \"$ALICE\",
    \"toWalletId\": \"$BOB\",
    \"amount\": 100
  }"
# → same {"transferId":"...","status":"PROCESSED"}
```

### Check balance

```bash
curl http://localhost:8080/wallets/$ALICE
# → {"id":"...","name":"Alice","balance":"900.00"}
```

## API

| Method | Path | Description |
|---|---|---|
| `POST` | `/transfers` | Create (or replay) a transfer |
| `POST` | `/wallets` | Create a wallet |
| `GET` | `/wallets/{id}` | Get wallet balance |
| `GET` | `/healthz` | Health check |

### Error codes

| Code | HTTP Status | Meaning |
|---|---|---|
| `INVALID_REQUEST` | 400 | Malformed JSON, negative/zero amount, same wallet |
| `WALLET_NOT_FOUND` | 404 | Source or destination wallet doesn't exist |
| `INSUFFICIENT_FUNDS` | 409 | Source balance too low |
| `IDEMPOTENCY_CONFLICT` | 409 | Key reused with different parameters |
| `INTERNAL_ERROR` | 500 | Unexpected server/database error |
=======
# Wallet Transfer Assignment Repository

This repository is a reusable coding assignment template for evaluating backend engineers on wallet transfers, idempotency, concurrency control, and double-entry ledger design.

## Included

- `ASSIGNMENT.md` - candidate-facing prompt
- `.github/pull_request_template.md` - required PR structure
- `.github/workflows/ci.yml` - lint, format, test placeholder workflow
- `.github/workflows/sonarqube.yml` - SonarQube pull request analysis
- `.github/copilot-instructions.md` - repository-level Copilot review guidance
- `evaluation_guide.md` - reviewer rubric
- `branch-protection-checklist.md` - GitHub setup checklist

## Intended use

1. Mark this repository as a GitHub template repository.
2. Create one private repository per candidate from the template.
3. Add the candidate as a collaborator.
4. Ask them to submit via a pull request into `main`.
5. Enable required checks, SonarQube, and Copilot review in GitHub.

## Notes

- Copilot automatic pull request review is configured in GitHub repository or organization settings, not purely through files in the repo.
- The `copilot-instructions.md` file included here provides repository-specific review guidance once Copilot review is enabled.
- The CI workflow is language-agnostic by default and expects you to set the `LINT_CMD`, `FORMAT_CHECK_CMD`, and `TEST_CMD` repository variables or replace the commands directly.

## How to Submit Assignment

1. **Fork this repository** to your own GitHub account.
2. Complete the assignment described in [`ASSIGNMENT.md`](./ASSIGNMENT.md).
3. **Raise a Pull Request** back to this repository (`main` branch) with your full solution.

Your PR branch should be named: `solution/<your-name>` (e.g., `solution/jane-doe`).
>>>>>>> 8b9736390e6545fd3608fc47df8a714613795f48

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

## Implementation

This repository contains a working Go implementation of the assignment.
See [`docs/DESIGN.md`](docs/DESIGN.md) for schema, idempotency, and
concurrency design notes.

### Requirements

- Go 1.27+
- Docker (for local PostgreSQL and Testcontainers), or any reachable
  PostgreSQL instance

### Run locally

```bash
make up                       # starts Postgres on localhost:5432
cp .env.example .env          # adjust if needed
make run                      # runs the API on :8080, applying migrations on boot
```

```bash
# create two wallets
curl -X POST localhost:8080/wallets -d '{"id":"wallet_1","initialBalance":1000}'
curl -X POST localhost:8080/wallets -d '{"id":"wallet_2","initialBalance":0}'

# transfer funds
curl -X POST localhost:8080/transfers -d '{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}'
```

### Test

```bash
make test              # service + HTTP tests with repository mocks
make test-integration  # starts disposable Postgres with Testcontainers
```

The integration tests require a running Docker daemon and start a disposable
PostgreSQL 16 container automatically. To use an already-running PostgreSQL
instance instead, set `TEST_DATABASE_URL` before running `make test-integration`.

### HTTPS configuration

The server uses HTTP by default. Set `SSL_ENABLED=true` to serve HTTPS. When
`TLS_CERT_FILE` and `TLS_KEY_FILE` are both provided, they configure the HTTPS
certificate. When they are omitted, the server generates an ephemeral
self-signed certificate for local development. The certificate and key must be
configured together when either is provided.

### Project layout

```
cmd/server                     entrypoint: config, wiring, HTTP server
internal/controller             transport: routing, DTOs, error mapping
internal/service                business logic: transfer workflow, idempotency
internal/domain                 entities, state machine, validation
internal/repository              repository interfaces + UnitOfWork
internal/repository/postgres     pgx-based production implementation
internal/migrations              embedded SQL schema migrations
```


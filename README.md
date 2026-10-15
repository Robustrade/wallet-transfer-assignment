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

---

## Wallet Transfer Service — Implementation

This branch contains a working implementation of the assignment. See
[`solution.md`](./solution.md) for the architecture, schema, idempotency,
and concurrency design.

### Requirements

- Go 1.24+
- Docker (for local PostgreSQL)

### How to Run

```bash
# start Postgres and apply migrations/001_init.sql on first boot
docker compose up -d postgres

# run the HTTP server (defaults to :8080, DATABASE_URL below)
go run ./cmd/server
```

Environment variables (both optional, shown with their defaults):

```text
DATABASE_URL=postgres://postgres:postgres@localhost:5432/wallet_transfer?sslmode=disable
HTTP_ADDR=:8080
```

Example requests:

```bash
curl -X POST localhost:8080/wallets -d '{"id":"wallet_1","startingBalance":1000}'
curl -X POST localhost:8080/wallets -d '{"id":"wallet_2","startingBalance":0}'
curl -X POST localhost:8080/transfers -d '{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
curl localhost:8080/wallets/wallet_1
curl localhost:8080/transfers/<id>
```

### How to Test

Tests run against a real PostgreSQL instance (they `t.Skip` automatically if
one isn't reachable):

```bash
docker compose up -d postgres
go test ./... -v
```

Set `TEST_DATABASE_URL` to point at a different database if needed.

### Lint / Format

```bash
go vet ./...
gofmt -l .      # should print nothing
```

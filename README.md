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

# Solution — Wallet Transfer Service (Go)

Go 1.24, standard library `net/http` + `log/slog`, `database/sql` with `lib/pq`, PostgreSQL 16.
Full design: [`docs/DESIGN.md`](./docs/DESIGN.md).

## Run

```bash
make db-up            # docker compose: Postgres 16 with databases `wallet` and `wallet_test`
make run              # applies migrations, listens on :8080
```

Try it:

```bash
curl -XPOST localhost:8080/wallets -H 'Content-Type: application/json' -d '{"id":"wallet_1","initialBalance":1000}'
curl -XPOST localhost:8080/wallets -H 'Content-Type: application/json' -d '{"id":"wallet_2"}'
curl -i -XPOST localhost:8080/transfers -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
curl localhost:8080/wallets/wallet_1
curl localhost:8080/wallets/wallet_2/ledger
```

## Test, lint, format

```bash
make test             # go test ./... -race -cover (integration tests need the Postgres above)
make lint             # golangci-lint run ./...
make fmt-check        # test -z "$(gofmt -l .)"
go test -short ./...  # unit tests only, no database
```

`DATABASE_URL`, `TEST_DATABASE_URL` and `PORT` can be overridden (see `.env.example`).

CI repository variables (same as the Go example in `branch-protection-checklist.md`):

- `LINT_CMD=golangci-lint run ./...`
- `FORMAT_CHECK_CMD=test -z "$(gofmt -l .)"`
- `TEST_CMD=go test ./... -race -cover` (needs Postgres via `TEST_DATABASE_URL`; use `-short` to run unit tests only)

## Layout

```text
cmd/server/                     main: config, migrations, HTTP server, graceful shutdown
internal/db/                    connection, WithTx, embedded migrations
internal/db/migrations/         schema, constraints, indexes, ledger triggers
internal/domain/                entities, state machine, validation, sentinel errors
internal/repository/            SQL only, no workflow decisions
internal/service/               transfer workflow, idempotency, transaction boundaries
internal/handler/               thin HTTP handlers, JSON decoding, error mapping
internal/testutil/              real-Postgres test setup
```

## API

| Method | Path | Notes |
|--------|------|-------|
| POST | `/transfers` | `201` PROCESSED, `422` FAILED (insufficient funds), `409` key reused with other body, `404` unknown wallet, `400` invalid. Replays return the original code and body with `Idempotent-Replayed: true` |
| GET | `/transfers/{id}` | one transfer |
| POST | `/wallets` | seed helper: `{ "id", "initialBalance" }` |
| GET | `/wallets/{id}` | balance |
| GET | `/wallets/{id}/ledger?limit=50` | latest ledger entries |
| GET | `/health` | DB check |

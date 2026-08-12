# Wallet Transfer Service

A small Go HTTP service that supports wallet-to-wallet transfers with
idempotent request handling, a double-entry ledger, and concurrency-safe
balance updates. Built for the coding assignment described in
[`ASSIGNMENT.md`](./ASSIGNMENT.md); the design rationale — schema,
idempotency strategy, locking strategy, alternatives considered — lives in
[`docs/design.md`](./docs/design.md), and the full API contract is in
[`docs/api.md`](./docs/api.md).

> This repository started life as [`devthedeveloper/wallet-transfer-assignment`](https://github.com/devthedeveloper/wallet-transfer-assignment), a reusable template for evaluating candidates on this exact problem (see the original template notes at the bottom of this file). What follows documents the solution built on that template.

## Architecture

```
 ┌─────────────────────────────────────────────────────────┐
 │  cmd/walletd            config -> connect -> migrate ->  │
 │                          wire services -> serve -> wait  │
 └───────────────────────────────┬───────────────────────────┘
                                  │
 ┌────────────────────────────── ▼ ───────────────────────────┐
 │  internal/httpapi   decode -> resolve idempotency key ->    │
 │  (handler layer)     call service -> map result to HTTP     │
 │  no business rules, no SQL                                  │
 └───────────────────────────────┬───────────────────────────┘
                                  │  service.TransferService / WalletService
 ┌────────────────────────────── ▼ ───────────────────────────┐
 │  internal/service    idempotency policy, transaction        │
 │  (service layer)      boundary, settle-or-fail workflow      │
 │  depends only on internal/service/ports.go interfaces        │
 └───────────────────────────────┬───────────────────────────┘
                                  │  service.UnitOfWork / Repos
 ┌────────────────────────────── ▼ ───────────────────────────┐
 │  internal/postgres    GORM-backed connection/tx management;  │
 │  (repository layer)    locking, guarded UPDATEs and the       │
 │                         idempotency claim stay raw SQL via    │
 │                         .Raw()/clause.Locking — see            │
 │                         docs/design.md §13                     │
 └───────────────────────────────┬───────────────────────────┘
                                  │
 ┌────────────────────────────── ▼ ───────────────────────────┐
 │  internal/domain      entities, state machine, validation,   │
 │  (domain layer)         zero infrastructure imports           │
 └───────────────────────────────────────────────────────────┘
```

## Quickstart

```bash
cp .env.example .env      # adjust if you need to
make db-up                # postgres:16-alpine via docker compose, port 5440
make run                  # applies migrations, serves on :8080
```

## Try it

```bash
BASE=http://localhost:8080

# create two wallets
curl -s -X POST $BASE/wallets -d '{"id":"wallet_1","currency":"INR"}'
curl -s -X POST $BASE/wallets -d '{"id":"wallet_2","currency":"INR"}'

# fund wallet_1 from the seeded treasury system wallet
curl -s -X POST $BASE/transfers -d '{
  "idempotencyKey":"fund-wallet_1","fromWalletId":"treasury_inr","toWalletId":"wallet_1","amount":10000
}'

# the assignment's example request
curl -s -X POST $BASE/transfers -d '{
  "idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100
}'

# replay the exact same request — identical body, Idempotent-Replay: true header, balances unchanged
curl -si -X POST $BASE/transfers -d '{
  "idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100
}'

curl -s $BASE/wallets/wallet_1
curl -s $BASE/wallets/wallet_2
curl -s $BASE/wallets/wallet_1/entries
```

Full endpoint reference and status-code map: [`docs/api.md`](./docs/api.md).

## Testing

```bash
make test-unit   # go test ./... -short — no database required
make test        # starts postgres, then the full suite with -race (unit + integration)
make cover       # same, with a coverage report
```

`test/integration` covers the concurrency-sensitive behavior directly: 25
simultaneous requests with the same idempotency key collapsing into one
transfer, 10 concurrent transfers racing a wallet funded for exactly 3 of
them, and 60 concurrent bidirectional transfers proving the lock ordering
is deadlock-free. Every integration test ends by asserting a set of global
invariants (closed-ledger sum is zero, every wallet's stored balance
matches its ledger sum, no negative user balance) — see
[`docs/design.md` §11](./docs/design.md#11-testing-strategy).

Integration tests read `TEST_DATABASE_URL` and skip cleanly if it's unset,
so `go test ./...` is always green even without Docker.

## Configuration

Set via environment variables (see `.env.example`):

| Variable | Default | Meaning |
|---|---|---|
| `DATABASE_URL` | *(required)* | Postgres connection string |
| `HTTP_ADDR` | `:8080` | listen address |
| `LOG_LEVEL` | `info` | slog level |
| `SHUTDOWN_TIMEOUT` | `10s` | graceful shutdown deadline |
| `DB_MAX_CONNS` | `10` | pool size |
| `DB_STATEMENT_TIMEOUT` | `5s` | per-statement timeout |
| `DB_LOCK_TIMEOUT` | `3s` | per-transaction `lock_timeout` |

## Repository-variable values this template's `ci.yml` expects

If you enable the template's own `ci.yml` (self-hosted runner) against this
solution, set these repository variables:

```
LINT_CMD=golangci-lint run ./...
FORMAT_CHECK_CMD=test -z "$(gofmt -l .)"
TEST_CMD=go test ./... -race -count=1 -p 1
```

`.github/workflows/solution-ci.yml` runs the same checks on a standard
`ubuntu-latest` runner against a `postgres:16-alpine` service container, so
CI is green without any repository configuration.

## AI usage

See [`docs/ai-usage.md`](./docs/ai-usage.md) and [`prompt.md`](./prompt.md)
for the full disclosure required by `ASSIGNMENT.md`.

---

## Original template notes

The sections below are preserved from the template this repository was
generated from, for reviewers who want the evaluator-side context.

### Included in the template

- `ASSIGNMENT.md` - candidate-facing prompt
- `.github/pull_request_template.md` - required PR structure
- `.github/workflows/ci.yml` - lint, format, test placeholder workflow
- `.github/workflows/sonarqube.yml` - SonarQube pull request analysis
- `.github/copilot-instructions.md` - repository-level Copilot review guidance
- `evaluation_guide.md` - reviewer rubric
- `branch-protection-checklist.md` - GitHub setup checklist

### Intended use

1. Mark this repository as a GitHub template repository.
2. Create one private repository per candidate from the template.
3. Add the candidate as a collaborator.
4. Ask them to submit via a pull request into `main`.
5. Enable required checks, SonarQube, and Copilot review in GitHub.

### Notes

- Copilot automatic pull request review is configured in GitHub repository or organization settings, not purely through files in the repo.
- The `copilot-instructions.md` file included here provides repository-specific review guidance once Copilot review is enabled.
- The CI workflow is language-agnostic by default and expects you to set the `LINT_CMD`, `FORMAT_CHECK_CMD`, and `TEST_CMD` repository variables or replace the commands directly.

### How to Submit Assignment

1. **Fork this repository** to your own GitHub account.
2. Complete the assignment described in [`ASSIGNMENT.md`](./ASSIGNMENT.md).
3. **Raise a Pull Request** back to this repository (`main` branch) with your full solution.

Your PR branch should be named: `solution/<your-name>` (e.g., `solution/jane-doe`).

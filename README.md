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

## Solution

Implemented with **Go + Gin**. See [`DESIGN.md`](./DESIGN.md) for the full
design note (schema, idempotency strategy, concurrency strategy, failure
modes) written before implementation, per the assignment's documentation
-first workflow.

### Layout

```
cmd/server/            entrypoint, dependency wiring
internal/domain/        entities, state machine, sentinel errors
internal/repository/    persistence ports (interfaces) + postgres/ implementation
internal/service/       business logic: idempotency, locking, ledger, transfer workflow
internal/handler/       Gin handlers, request/response DTOs, error mapping
internal/router/        route registration + request logging middleware
internal/db/            connection + embedded SQL migrations
internal/config/        env-based configuration
test/                   Postgres-backed integration + concurrency tests
```

### Run it

```bash
cp .env.example .env
make run          # starts Postgres via docker compose, then the API on :8080
```

Demo wallets `wallet_1` (balance `50000`), `wallet_2` (balance `20000`),
`wallet_3` (balance `0`) are seeded automatically by migration
`0002_seed_demo_wallets`. Balances are integers in the wallet's smallest
currency unit (e.g. cents), same as every `amount` in the API -- so
`wallet_1`'s `50000` is $500.00, not $50,000.00.

```bash
curl -s -X POST localhost:8080/transfers \
  -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
```

### Test it

```bash
make test-unit          # service-layer tests against in-memory fakes, no DB needed
make test-integration    # real Postgres: constraints, row locking, concurrency
```

### API

| Method | Path                  | Purpose                                   |
|--------|-----------------------|--------------------------------------------|
| POST   | `/transfers`           | create a transfer (idempotent)             |
| GET    | `/transfers/:id`       | fetch a transfer                           |
| POST   | `/wallets`              | create a wallet (not in the spec; needed to seed/test the system) |
| GET    | `/wallets/:id`          | wallet balance                             |
| GET    | `/wallets/:id/ledger`   | ledger entries for a wallet (transfer history) |
| GET    | `/healthz`              | liveness                                   |

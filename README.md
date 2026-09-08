# Wallet Transfer Service

Runnable Go implementation of the wallet transfer assignment.

## Run

```text
go run ./cmd/server
```

The service listens on `:8080` by default. Set `HTTP_ADDR` and `DATABASE_PATH` to override the address and SQLite file.

Prometheus metrics are exposed at `GET /metrics`. Start the Prometheus and Grafana stack from the repository root with:

```text
docker compose -f observability/docker-compose.yml up
```

Prometheus is available at `http://localhost:9090` and Grafana at `http://localhost:3000`. The provisioned Grafana dashboard includes HTTP request rate, p95 latency, and transfer outcomes. Run the Go service on the host first so Prometheus can scrape `host.docker.internal:8080`.

Create wallets with `POST /wallets`, then transfer with `POST /transfers`:

```json
{"id":"wallet_1","balanceCents":1000}
```

```json
{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}
```

`GET /wallets` returns all wallets, `GET /wallets/{id}` returns one current balance, `GET /ledgers` returns all ledger entries, and `GET /transfers/{id}` returns the ledger entries for one transfer.

## Design

- SQLite transactions use an immediate write lock, so concurrent debits serialize safely.
- The unique `transfers.idempotency_key` constraint stores the original result and prevents duplicate side effects.
- A transfer is inserted as `PENDING`, then either becomes `FAILED` for insufficient funds or `PROCESSED` alongside exactly one debit and one credit ledger entry.
- Wallet balances are stored in cents and updated in the same transaction as the ledger.
- A reused idempotency key with a different payload is rejected.

Run the tests with:

```text
go test ./...
```

---

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

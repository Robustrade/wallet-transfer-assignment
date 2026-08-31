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

## Solution: Wallet Transfer Service

This submission implements a Go HTTP service with SQLite persistence. The required
endpoint is `POST /transfers`; details of its transaction, idempotency, and concurrency
strategy are in [DESIGN.md](DESIGN.md).

### Run

Requires Go 1.24 or newer:

```sh
go run ./cmd/server
```

The service listens on `http://localhost:8080` and uses `wallets.db` by default. Set
`DATABASE_URL` to a SQLite file or DSN to select another database. Wallets are seeded
through the repository for this deliberately focused assignment.

### Example

```sh
curl -X POST http://localhost:8080/transfers -H "Content-Type: application/json" -d '{"idempotencyKey":"transfer-001","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
```

The response is `201 Created` for a processed transfer. Repeating the same request
returns the original transfer with `replayed: true`; reusing a key for different input
returns `409 Conflict`.

### Test

```sh
go test ./...
```

Tests cover transfers, idempotent replay and conflicts, insufficient funds,
double-entry creation, and competing debits.

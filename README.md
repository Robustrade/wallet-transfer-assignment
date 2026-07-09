# Wallet Transfer Assignment Repository

This branch contains a Go implementation of the wallet transfer assignment. It provides a small HTTP service with SQLite persistence, idempotent transfer creation, stored wallet balances, safe transfer state transitions, and double-entry ledger entries.

## Running locally

Requirements:

- Go 1.24
- CGO-enabled SQLite build support. On Linux, install `gcc`; on macOS, Xcode command line tools are enough.

Run tests:

```sh
go test ./...
```

Run the service:

```sh
go run ./cmd/server
```

By default the service listens on `:8080` and uses `wallets.db`. Override with:

```sh
HTTP_ADDR=:9090 DB_PATH=/tmp/wallets.db go run ./cmd/server
```

Seed wallets are created if absent:

- `wallet_1` with balance `1000`
- `wallet_2` with balance `1000`
- `wallet_3` with balance `1000`

Create a transfer:

```sh
curl -X POST http://localhost:8080/transfers \
  -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
```

Read data:

```sh
curl http://localhost:8080/wallets/wallet_1
curl http://localhost:8080/transfers/<transfer_id>
```

## Implementation notes

- Architecture is split into handler, service, repository, domain, and port packages.
- SQLite is used for persistence to keep the assignment self-contained.
- Transfers are executed inside a database transaction.
- Idempotency records store the key, canonical request hash, and original transfer id.
- Duplicate requests with the same key and payload return the original result without side effects.
- Duplicate requests with the same key and different payload return a conflict.
- Wallet debits use a conditional update to prevent overspending.
- Processed transfers write exactly two ledger entries: one `DEBIT` and one `CREDIT`.
- Failed business transfers are durable and do not change balances or write ledger entries.

See [docs/DESIGN.md](./docs/DESIGN.md) for the detailed design, failure modes, and testing strategy.

## Original template content

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

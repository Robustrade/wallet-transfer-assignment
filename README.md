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

## Solution: Wallet Transfer Service (Go)

See [`DESIGN.md`](./DESIGN.md) for the schema, idempotency strategy, and
concurrency strategy.

### Run locally

```bash
make up       # start Postgres via docker-compose
make migrate  # apply schema migrations
make seed     # seed wallet_1, wallet_2, wallet_3 for trying the API
make run      # start the server on :8080
```

### Test

```bash
make test     # starts Postgres, migrates, then `go test ./... -race -cover`
```

Tests in `internal/service` are integration tests against a real Postgres
instance — the locking and idempotency strategy is exactly what needs
exercising against a real database, not a mock.

### CI repository variables

For this Go solution, set:

- `LINT_CMD=golangci-lint run ./...`
- `FORMAT_CHECK_CMD=test -z "$(gofmt -l .)"`
- `TEST_CMD=make test`

`make test` needs a reachable Postgres (it runs `docker compose up`). If the
CI runner doesn't support docker-compose, add a `postgres:16-alpine`
service container to `ci.yml` and point `TEST_CMD` at
`go run -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@v4.19.1 -database "$DATABASE_URL" -path migrations up && go test ./... -race -cover`
instead.

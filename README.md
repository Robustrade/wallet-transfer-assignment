# Wallet Transfer Assignment Repository

This repository is a reusable coding assignment template for evaluating backend engineers on wallet transfers, idempotency, concurrency control, and double-entry ledger design.

---

# Solution — Wallet Transfer Service

A backend-only, idempotent, concurrency-safe wallet-to-wallet transfer service
in Go + PostgreSQL. Full design rationale: [`APPROACH.md`](./APPROACH.md).
Working conventions for this codebase: [`CLAUDE.md`](./CLAUDE.md).

## Stack

- **Go 1.24**, stdlib `net/http` `ServeMux` (no web framework)
- **PostgreSQL** via `jackc/pgx` v5 + `pgxpool`, raw SQL (no ORM)
- **Goose** SQL migrations (`/migrations`)
- `shopspring/decimal` for all money math (never `float64`)
- Layered: `cmd/server → handler → service → repository → domain`

## Prerequisites

- Go 1.24+
- A PostgreSQL database (developed against Neon). Copy `.env.example` to
  `.env` and set `DATABASE_URL` (the value may be quoted; the Makefile/config
  strip surrounding quotes):

  ```
  DATABASE_URL=postgresql://USER:PASSWORD@HOST/DB?sslmode=require
  ```

`make tools` installs pinned `goose` + `golangci-lint` into `./bin`
(gitignored). Goose is intentionally **not** a Go module dependency (its
driver matrix would bloat `go.mod` and force Go ≥ 1.25, breaking CI).

## How to Run

```bash
make migrate-up      # apply schema migrations to $DATABASE_URL
make run             # start the HTTP server (PORT, default 8080)
```

Endpoints:

| Method | Path | Notes |
|---|---|---|
| `POST` | `/transfers` | `201` new / `200` duplicate key; body `{idempotencyKey, fromWalletId, toWalletId, amount}` |
| `GET` | `/transfers/{id}` | `200` / `404` |
| `GET` | `/healthz` | `200` if DB reachable |

`400` = malformed body / non-positive amount / same source==destination.
`404` = wallet or transfer not found. **Insufficient funds is not an error**:
`201`/`200` with `status: FAILED` and `failureReason` (the attempt is audited).

```bash
curl -X POST localhost:8080/transfers \
  -d '{"idempotencyKey":"k1","fromWalletId":"w1","toWalletId":"w2","amount":100}'
```

## How to Test

```bash
make test     # unit tests only (DATABASE_URL blanked; integration self-skips)
make itest    # integration + concurrency tests, race detector, needs DATABASE_URL
make lint     # golangci-lint (pinned, matches CI)
make fmt-check # gofmt verification
```

Unit tests use in-memory fakes (no DB). Integration/concurrency tests run
against the real database with uniquely generated ids and per-test cleanup, so
they are safe on a shared instance; they `t.Skip` when `DATABASE_URL` is unset
(which is why CI, having no Postgres, still passes lint/format/unit).

## CI repository variables

`.github/workflows/ci.yml` runs lint/format/test via repo variables. Set:

- `LINT_CMD=golangci-lint run ./...`
- `FORMAT_CHECK_CMD=test -z "$(gofmt -l $(git ls-files --cached --others --exclude-standard '*.go'))"`
- `TEST_CMD=go test ./...`
- `CGO_ENABLED=0`

---

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

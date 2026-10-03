# Wallet Transfer Service

A Go service on PostgreSQL that moves money between wallets through
`POST /transfers`. It solves the [assignment](ASSIGNMENT.md):

- **Idempotency:** every request carries an `idempotencyKey`, stored with
  `UNIQUE` on the transfer; a repeat returns the original response.
- **Double-entry ledger:** a `PROCESSED` transfer writes one DEBIT and one
  CREDIT entry.
- **Concurrency:** each transfer runs in one transaction that locks both
  wallets in ID order, so concurrent debits never overspend.

The template's original README follows [below](#wallet-transfer-assignment-repository).

## Docs

| Doc | Covers |
|-----|--------|
| [Requirements](docs/requirements.md) | Problem, requirements, and decisions D-1 to D-20 |
| [API spec](docs/api-spec.md) | `POST /transfers`: request, responses, error codes, idempotency |
| [Design](docs/design.md) | Layout, schema, transfer flow, locking, failure modes, tests |
| [ADRs](docs/adr/README.md) | The eight main design decisions |
| [Implementation plan](docs/implementation-plan.md) | Order of work and the Red → Blue commits |

## Run

Needs Docker and Go 1.24.

```sh
docker compose up -d
DATABASE_URL=postgres://postgres:postgres@localhost:5432/wallet go run ./cmd/server
```

On startup the server applies the schema and seeds three wallets:
`wallet_1` (1000), `wallet_2` (1000), and `wallet_3` (0). It listens on
`:8080`; set `HTTP_ADDR` to change that.

```sh
curl -i -X POST localhost:8080/transfers \
  -d '{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
```

The response is `201` with `{"transferId":"…","state":"PROCESSED"}`; sending
the same request again returns the same response. Stop the server with
Ctrl+C, then run `docker compose down`.

## Test

```sh
go test ./... -race -cover
```

- No external service is needed: `internal/apitest` starts its own Postgres 16
  with embedded-postgres.
- The first run downloads the Postgres binaries, so it needs network access
  and takes longer. They are cached in `~/.embedded-postgres-go`.
- Postgres refuses to run as root, so run the tests as a regular user.

The API tests live in their own package, so `-cover` reports 0% for
`handler`, `service`, and `repository` even though the API tests run them. To
count coverage across packages:

```sh
go test ./... -coverpkg=./internal/...
```

CI also runs `golangci-lint run ./...` (v1.64.8) and `test -z "$(gofmt -l .)"`.

## Layout

```text
cmd/server/            wiring: config, pool, schema and seed, HTTP server
internal/domain/       request rules, state transitions, ledger entries
internal/service/      the transfer workflow, in one transaction
internal/repository/   Postgres access; sql/schema.sql and sql/seed.sql
internal/handler/      HTTP decoding, error mapping, request logging
internal/apitest/      API tests against embedded Postgres
docs/                  requirements, API spec, design, ADRs, plan
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

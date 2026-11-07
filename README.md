# Wallet Transfer Service

A wallet-to-wallet transfer API in **Go 1.24 + PostgreSQL 16** with idempotent requests, a double-entry ledger
and safe concurrent debits. Solution to [`ASSIGNMENT.md`](./ASSIGNMENT.md); design and trade-offs are in
[`docs/design.md`](./docs/design.md).

## Quick start

Requirements: Go 1.22+ (the `go.mod` toolchain line fetches Go 1.24 automatically), Docker, `make`.

```sh
make up      # start Postgres 16 (docker compose)
make run     # start the API on :8080; applies migrations on startup
make seed    # in another terminal: wallet_1 = 10000, wallet_2 = 5000, wallet_3 = 0
```

Make a transfer (send it twice to see the replay):

```sh
curl -i -X POST localhost:8080/transfers -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"demo-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
```

`make down` stops Postgres and deletes its volume.

| Variable | Default | |
|---|---|---|
| `DATABASE_URL` | none (required) | `make run` sets it for the docker compose database |
| `HTTP_ADDR` | `:8080` | |

## API

### `POST /transfers`

```json
{ "idempotencyKey": "abc123", "fromWalletId": "wallet_1", "toWalletId": "wallet_2", "amount": 100 }
```

- `amount` is a positive integer in minor units (e.g. cents).
- `idempotencyKey` is optional (1–255 chars). Without it, requests are not deduplicated.
- Both wallets must already exist and must be different. Wallets are never created by a transfer.

| Outcome | Status | Body |
|---|---|---|
| Transfer processed | `201` | transfer, `status: PROCESSED` |
| Insufficient funds (`balance < amount`) | `422` | transfer, `status: FAILED`, `failureReason: INSUFFICIENT_FUNDS` |
| Same key, same payload | original status and body, header `Idempotent-Replayed: true` | original transfer |
| Same key, different payload | `409` | `IDEMPOTENCY_KEY_REUSED` |
| Invalid request | `400` | `VALIDATION_ERROR` |
| Unknown wallet | `404` | `WALLET_NOT_FOUND` |
| Lock timeout / temporary failure | `503` + `Retry-After` | `RETRYABLE`; safe to retry with the same key |

Transfer body:

```json
{
  "id": "8a49b129-2b67-4901-926c-fc3581789251",
  "idempotencyKey": "demo-1",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100,
  "status": "PROCESSED",
  "failureReason": null,
  "createdAt": "2026-11-07T10:18:18.113638Z"
}
```

Errors: `{"error": {"code": "WALLET_NOT_FOUND", "message": "..."}}`

`GET /healthz` returns `200` when the database is reachable.

## How it works

One database transaction per transfer:

1. **Lock** both wallets with `SELECT … ORDER BY id FOR NO KEY UPDATE`. Locking in a fixed order prevents
   deadlocks, and concurrent debits of the same wallet are serialised. A missing wallet returns `404`.
2. **Claim the idempotency key** with `INSERT … ON CONFLICT (idempotency_key) DO NOTHING`. If the key is already
   taken, return the stored transfer (or `409` if the payload differs). The unique index, not application code,
   decides between concurrent duplicates.
3. **Check funds.** If `balance < amount`, store the transfer as `FAILED` and commit. Retries with that key
   return the same failure.
4. **Move money:** debit, credit, write the DEBIT/CREDIT ledger pair and mark the transfer `PROCESSED`.

The database enforces the invariants as well: `CHECK (balance >= 0)`, a unique key, foreign keys, and
`UNIQUE (transfer_id, entry_type)` on ledger entries. See [`docs/design.md`](./docs/design.md) for the schema,
the deadlock analysis and the alternatives considered.

```
cmd/server/                    wiring, config, graceful shutdown
internal/handler/              HTTP decode → service → status codes (no business rules)
internal/service/              transfer workflow and idempotency; owns the transaction
internal/domain/               entities, validation, status rules, ledger pair (no I/O)
internal/repository/           interfaces the service depends on
internal/repository/postgres/  SQL; maps Postgres errors to domain errors
migrations/                    schema, embedded and applied at startup
```

## Tests

```sh
make test        # go test ./... -race -cover
make lint        # golangci-lint v1.64.8 (same version as CI)
make fmt-check   # gofmt
```

- Repository and service tests run against **real Postgres**: a disposable container via testcontainers
  (Docker required), or an existing database via `TEST_DATABASE_URL`. Each test package uses its own temporary
  schema, so existing tables are untouched.
- `go test ./... -short` runs only the domain and handler tests, with no database.
- Covered: success, insufficient funds (including a retry after a top-up), unknown wallets, validation, replay,
  key reuse with a different payload, requests without a key, and concurrency (same key ×20, 50 racing debits,
  200 overlapping and opposing transfers without deadlock). After every service test, a ledger check verifies
  that each processed transfer has one matching DEBIT and CREDIT and each balance equals its opening balance plus
  credits minus debits.

### CI

The workflow in `.github/workflows/ci.yml` reads its commands from repository variables:

| Variable | Value |
|---|---|
| `LINT_CMD` | `make lint` |
| `FORMAT_CHECK_CMD` | `make fmt-check` |
| `TEST_CMD` | `make test` (needs Docker on the runner), or set `TEST_DATABASE_URL` to an available Postgres |

---

## About this repository

This repository started as a reusable coding assignment template for evaluating backend engineers on wallet
transfers, idempotency, concurrency control, and double-entry ledger design.

### Included

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

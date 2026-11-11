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

## Solution — Wallet Transfer Service (Go)

**Status:** project scaffolding and tooling only. The service, migrations, seed data, and tests are not implemented yet. The behavior below describes our proposed solution.

### Run

The intended startup flow is to apply versioned SQL migrations separately, seed wallets with opening balances, configure `DATABASE_URL`, `HTTP_HOST`, and `HTTP_PORT`, then start the server:

```sh
make run              # go run ./cmd/server; requires the server implementation
```

The proposed local listener is `localhost:8080`. Startup validates configuration, database connectivity, and schema compatibility. Shutdown drains active HTTP requests before closing the database pool.

Database provisioning, migrations, and seed commands will be documented when implemented. There is currently no Compose configuration, `db-up` target, or `.env.example`; environment variables are the proposed configuration source.

### Try it

Once implemented and wallets `wallet_1` and `wallet_2` have been seeded, with at least 100 minor units in the source wallet:

```sh
curl -i -X POST http://localhost:8080/transfers \
  -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
```

Repeat the same request to replay its saved terminal status and body without moving money again. Reusing `abc123` with a different source, destination, or amount returns `409`.

Only `POST /transfers` is in the required API scope. Wallet creation, balance, and ledger endpoints are excluded from this solution; wallets are initialized through seed data.

### Architecture and transfer flow

```text
HTTP handler -> Transfer service -> PostgreSQL repository -> PostgreSQL
                       |
                  Domain models
```

Handlers decode and validate requests and map errors to HTTP responses. The service owns business rules, idempotency, and transaction boundaries. Repositories execute SQL through explicit transactions; domain models define entities and allowed state transitions.

The data model contains `wallets`, `transfers`, `ledger_entries`, and `idempotency_records`. Amounts are positive integer minor units in one currency. Stored wallet balances support fast checks; the append-only ledger records the audit trail.

1. Validate the request and hash the canonical source, destination, and amount. An existing key must match this payload before any result is replayed.
2. In a short reservation transaction, create a `PENDING` transfer and a unique idempotency record. A key conflict rolls back the tentative transfer and resolves the winning record.
3. In a processing transaction, lock the transfer row. Replay terminal results; otherwise lock both wallet rows individually in ascending ID order using `FOR NO KEY UPDATE` under `READ COMMITTED` isolation.
4. Recheck funds while holding the locks. Success atomically updates both balances, writes one debit and one credit, marks `PROCESSED`, and saves the terminal response. Insufficient funds saves `FAILED` and its response without changing balances or ledger entries.
5. Return a terminal outcome after confirmed commit. A retry resumes a durable `PENDING` transfer. If commit acknowledgement or the HTTP response is lost, the same key resolves the persisted outcome without another financial effect.

Database constraints protect nonnegative balances, valid relationships, positive amounts, and unique ledger types per transfer. Deferred ledger validation and immutability guards protect matching ledger pairs and terminal states. Reconciliation checks that each stored balance equals its opening balance plus credits minus debits.

| Outcome | HTTP behavior |
| --- | --- |
| Successful transfer | `201`, persisted `PROCESSED` result |
| Insufficient funds | `422`, persisted `FAILED` result |
| Invalid request / missing wallet | `400` / `404`, no key reserved |
| Key reused with different payload | `409`, no financial effect |
| Terminal duplicate | Original status and body, `Idempotent-Replayed: true` |
| Unresolved concurrent processing | `202` with `Retry-After` |
| Database outage or lock timeout | Retryable `503` |
| Unexpected internal failure | Generic `500` |

Transient failures and `202` responses are not cached as terminal results. Clients retry the same key and payload with backoff. Database waits are bounded; an internal transient-error retry restarts the entire affected transaction.

### Test, lint, format

These targets exist in the Makefile; package-dependent commands require Go source and tests to be added first:

```sh
make test             # go test -race -v -coverprofile=profiles/coverage.out ./...
make lint             # golangci-lint run
make fmt-check        # checks gofmt/goimports without rewriting source
make fmt              # applies configured formatting
make vet              # go vet ./...
make check            # formatting check, lint, vet, race tests
```

Use golangci-lint v2 with `.golangci.yml`. The Makefile requires GNU Make, Bash, and Unix utilities; on Windows use a compatible environment such as MSYS2. Race tests require CGO and a compatible C compiler.

Tests will cover validation and HTTP mapping, successful and failed transfers, balanced ledger entries, replay, conflicting payloads, concurrent duplicates/debits, opposite-direction transfers, rollback, and pending recovery against a dedicated PostgreSQL database. Test database configuration and unit-only selection will be documented with the test implementation.

CI repository variables can use:

```text
LINT_CMD=make lint
FORMAT_CHECK_CMD=make fmt-check
TEST_CMD=make test
CGO_ENABLED=1
```

The existing CI workflow selects Go 1.24 and requires its configured self-hosted Linux runner. It installs golangci-lint v1.64.8, which is incompatible with the local v2 configuration; its installer command also needs a separate PATH-update line. These CI issues remain unresolved because `ci.yml` is being kept unchanged. SonarQube requires `SONAR_TOKEN` and `SONAR_HOST_URL`.

### Tradeoffs

| Decision | Benefit | Cost or limitation |
| --- | --- | --- |
| PostgreSQL as the consistency boundary | Durable transactions, uniqueness, and locks across service instances | Requires PostgreSQL for execution and realistic integration tests |
| Separate reservation and processing transactions | Durable, recoverable `PENDING` state | Additional round trip and recovery logic |
| Recovery on client retry | No background worker required | Abandoned transfers remain pending until retried |
| Stored balances plus append-only ledger | Fast balance checks and an audit trail | Both representations must be updated atomically and reconciled |
| Deterministic wallet row locks | Prevents overspending and opposite-direction deadlocks | Busy wallets serialize transfers; contention may require retries |
| Database ledger and state guards | Protects committed invariants | Trigger logic adds complexity and needs direct tests |
| Indefinite key retention and saved responses | Stable replay across restarts and lost responses | Storage grows; a failed key stays failed even after funds change |
| Integer minor units, one currency | Exact arithmetic | Requires overflow checks; no currency conversion |

The design provides at most one committed financial effect per retained key. HTTP delivery itself cannot be guaranteed exactly once. Keys are globally scoped in a trusted assignment environment, and wallets are not physically deleted.

Extra wallet/history APIs, background workers, distributed locks, metrics infrastructure, public profiling endpoints, external payment gateways, and multi-region writes are outside scope.

### Project structure

```text
cmd/server/                    Entry point and dependency wiring
internal/
  config/                      Environment configuration
  domain/                      Entities, states, and domain errors
  handler/                     HTTP contract and error mapping
  middleware/                  Request IDs, logging, panic recovery
  service/                     Transfer workflow and transaction ownership
  repository/postgres/         SQL and database access
migrations/                    Versioned PostgreSQL schema migrations
tests/integration/testdata/    Integration fixtures and seed data
```

Unit and HTTP tests live beside their packages as `*_test.go` files. `.gitkeep` files preserve empty directories until implementation files are added.

# Wallet Transfer Service

A small Go + PostgreSQL service for **wallet-to-wallet transfers** with idempotent request handling, a double-entry ledger, and race-free balances. Design rationale is in [`APPROACH.md`](./APPROACH.md); the assignment is in [`ASSIGNMENT.md`](./ASSIGNMENT.md).

## Highlights

- **Exactly-once at the API level.** The `idempotencyKey` is claimed inside the same transaction as the money movement; duplicates replay the original result.
- **No double spend.** Both wallets are row-locked (`SELECT ... FOR UPDATE`) in a deterministic order, and the database refuses a negative balance (`CHECK (balance >= 0)`).
- **Double-entry ledger.** Every processed transfer writes exactly one `DEBIT` and one `CREDIT`; the ledger is append-only and balance-checked by the database at commit.
- **Safe state machine.** `PENDING -> PROCESSED | FAILED`, enforced in the domain and by a compare-and-set `UPDATE`.
- **Layered.** `handler -> service -> repository -> PostgreSQL`, with business rules in `domain`.

## Run it

Requirements: Go 1.24+ and a PostgreSQL instance.

```bash
export DATABASE_URL="postgres://user:pass@localhost:5432/wallets?sslmode=disable"
export HTTP_ADDR=":8080"   # optional, default :8080
go run ./cmd/server        # applies migrations/ on startup
```

Try it (amounts are integers in minor units, e.g. paise/cents):

```bash
curl -X POST localhost:8080/wallets -d '{"id":"wallet_1","balance":500}'
curl -X POST localhost:8080/wallets -d '{"id":"wallet_2","balance":0}'

curl -i -X POST localhost:8080/transfers -d '{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}'
# 201 Created. Send it again: 200 OK, header Idempotent-Replayed: true, same transfer id.

curl localhost:8080/wallets/wallet_1      # balance 400
curl localhost:8080/transfers/<id>        # transfer + its two ledger entries
```

## API

| Method & path | Purpose | Responses |
|---|---|---|
| `POST /transfers` | Create a transfer (key in body `idempotencyKey`, or `Idempotency-Key` header) | `201` processed, `200` replay of an earlier result, `422` insufficient funds / key reused with a different payload, `404` unknown wallet, `400` invalid input |
| `GET /transfers/{id}` | Transfer with its ledger entries | `200`, `404`, `400` |
| `POST /wallets` | Create a wallet with an opening balance (for seeding) | `201`, `409` exists, `400` |
| `GET /wallets/{id}` | Current balance | `200`, `404` |
| `GET /healthz` | Liveness | `200` |

A failed business outcome (e.g. insufficient funds) is **recorded** as a `FAILED` transfer and replayed with the same `422` for that key. To try again, use a new key. Infrastructure errors roll everything back, including the key claim, so retrying with the same key is safe.

## Test

```bash
go test ./...        # or: make test
```

The integration tests need a real PostgreSQL. They start an **embedded PostgreSQL automatically** (downloaded and cached on first run, internet required once), so no Docker or manual setup is needed. To use your own server instead:

```bash
export TEST_DATABASE_URL="postgres://user:pass@localhost:5432/postgres?sslmode=disable"
```

Each test creates and drops its own database. Other checks: `make lint` (golangci-lint), `make fmt-check` (gofmt), `make test-race` (needs a C toolchain).

Suggested values for the CI repository variables: `LINT_CMD="golangci-lint run ./..."`, `FORMAT_CHECK_CMD='test -z "$(gofmt -l .)"'`, `TEST_CMD="go test -race ./..."`.

What the tests cover:

- **Domain:** validation, state machine, balanced double-entry shape.
- **Schema:** the database itself rejects overdrafts, duplicate keys, orphan or unbalanced ledger rows, and ledger edits.
- **Transfers:** ledger correctness, insufficient funds, unknown wallets, rollback after a mid-transaction failure.
- **Idempotency:** sequential and concurrent duplicates, key reuse, replayed failures, key release after a partial failure.
- **Concurrency:** 50 simultaneous debits of one wallet (exactly the affordable number succeed), opposing transfers (no deadlock), random transfers (money is conserved), and a ledger/balance reconciliation after each.
- **HTTP:** the status-code contract and the exactly-once behaviour end to end.

## Design summary

**Schema** (`migrations/0001_init.sql`): `wallets`, `transfers`, `ledger_entries`, `idempotency_records`. Balances are stored (`BIGINT` minor units) and reconcile with the ledger: `balance = opening_balance + credits - debits`. Constraints do the heavy lifting: `CHECK (balance >= 0)`, `UNIQUE (idempotency_key)`, foreign keys, `UNIQUE (transfer_id, type)`, an append-only ledger trigger and a deferred trigger requiring one equal DEBIT/CREDIT pair per transfer.

**Idempotency:** `INSERT INTO idempotency_records ... ON CONFLICT DO NOTHING` claims the key as the first step of the transfer transaction. A concurrent duplicate blocks on the unique index until the first transaction finishes, then replays its stored transfer. A request hash detects the same key with a different payload.

**Concurrency:** one `READ COMMITTED` transaction per transfer; wallets locked `ORDER BY id FOR UPDATE`, so opposite-direction transfers cannot deadlock; transient deadlock/serialization errors are retried by re-running the transaction.

**Layout**

```text
cmd/server/                 entry point and wiring
internal/domain/            entities, state machine, validation
internal/service/           transfer workflow, idempotency, transactions
internal/repository/        persistence contracts
internal/repository/postgres/   pgx implementation, migrations runner
internal/handler/           HTTP transport
internal/testutil/          embedded PostgreSQL for tests
internal/integration/       tests against real PostgreSQL
migrations/                 SQL schema
```

## Trade-offs and assumptions

- `idempotencyKey` is required and globally unique here; a real system scopes it per client and expires old keys.
- A failed business outcome is final for its key (retry with a new key). An unknown wallet is rejected without being recorded, so the same key works once the wallet exists.
- Processing is synchronous inside one transaction: simple and fully atomic, at the cost of holding two row locks briefly. A very hot wallet serialises its transfers by design.
- Concurrent duplicates wait on the first request and hold a database connection while waiting; production would add a statement/lock timeout and connection-pool limits.
- No authentication, multi-currency, or key-expiry job (out of scope).

---

## Assignment template notes

The sections below are from the original assignment repository.

### How to Submit Assignment

1. **Fork this repository** to your own GitHub account.
2. Complete the assignment described in [`ASSIGNMENT.md`](./ASSIGNMENT.md).
3. **Raise a Pull Request** back to this repository (`main` branch) with your full solution.

Your PR branch should be named: `solution/<your-name>` (e.g., `solution/jane-doe`).

### CI notes

The CI workflow expects the `LINT_CMD`, `FORMAT_CHECK_CMD`, and `TEST_CMD` repository variables; repository variables are not inherited by forks.

# Wallet Transfer Service

A wallet-to-wallet transfer API in Go and PostgreSQL, built so that **every
guarantee a constraint can express is expressed as a constraint**. Idempotency is
a `UNIQUE` constraint, not a check-then-insert. An overdraft is a `CHECK`, not an
`if`. A second debit on one transfer is `UNIQUE (transfer_id, type)`, not a
convention.

One invariant needs more than a constraint: that a transfer's two ledger entries
are both present and balanced. That one is held by construction in the domain,
and [`docs/design.md`](docs/design.md) says exactly where the
schema stops.

The full design is in **[`docs/design.md`](docs/design.md)**: the API contract,
the transaction shape, the failure modes, and for each decision the alternative
that was rejected.

---

## Quick start

Requires Go 1.24+ and Docker.

Start PostgreSQL and apply the schema:

```bash
docker compose up -d --wait
export DATABASE_URL='postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable'
go run ./cmd/migrate
```

Wallets are assumed to exist. Nothing in scope creates one, so seed a couple
before starting the service, while you still have a prompt:

```bash
psql "$DATABASE_URL" -c "INSERT INTO wallets (id, balance) VALUES
  ('wallet_1', 1000), ('wallet_2', 0);"
```

Now run the service. It migrates on boot, listens on `:8080`, and stays in the
foreground until you stop it, so run the `curl` below from another terminal:

```bash
go run ./cmd/server
```

```bash
curl -i -X POST localhost:8080/transfers \
  -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
```

Send it **twice**. The second call returns `200` with `Idempotent-Replay: true`
and a byte-identical body, and the money moves only once.

---

## API

### `POST /transfers`

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

Decoding is strict:

- `amount` is an integer in **minor units** (cents), never a float. `100.5` is
  rejected at decode rather than truncated.
- An unknown field is rejected rather than ignored, so a misspelled `"ammount"`
  fails loudly instead of sending a transfer of zero.

Every outcome that produced a transfer returns the same body:

```json
{
  "id": "9f8c1a4e-0f1e-4c2b-9a77-6b2d1f0c5a31",
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100,
  "state": "PROCESSED",
  "failureReason": null,
  "createdAt": "2026-09-14T10:23:03Z"
}
```

`state` is the authoritative answer. A client never has to read the status code
to learn what happened to its money. That matters, because the status code is the
part most likely to be rewritten by a proxy.

| Status | When |
|---|---|
| `201 Created` | A new transfer was created and processed |
| `200 OK` | Replay of a processed key. Identical body, `Idempotent-Replay: true` |
| `400 Bad Request` | Malformed JSON, unknown field, or failed validation |
| `404 Not Found` | A wallet does not exist |
| `409 Conflict` | Key already used with **different** parameters |
| `422 Unprocessable Entity` | Insufficient funds, on the first call and every replay |
| `503 Service Unavailable` | Lock timeout; safe to retry unchanged |

Conditions that produced no transfer return an error object instead:

```json
{ "error": { "code": "WALLET_NOT_FOUND", "message": "wallet wallet_9 does not exist" } }
```

---

## Design decisions

Five choices worth knowing before reading the code. Each is argued in full, with
its rejected alternative, in [`docs/design.md`](docs/design.md).

1. **Stored balance, not derived.** `CHECK (balance >= 0)` makes an overdraft
   impossible to *store*. The ledger remains the audit record, and a test
   asserts the two always agree.
2. **Pessimistic locking.** Both wallet rows are locked before the balance is
   read, so the balance checked is the balance written against. Locks are taken
   in ascending wallet-id order, sorted in Go rather than with `ORDER BY`, so
   the ordering is a property of the code and not of the query plan.
3. **Idempotency by unique constraint.** The key is claimed with
   `INSERT ... ON CONFLICT (idempotency_key) DO NOTHING`, so the database
   arbitrates between concurrent duplicates rather than application code. A
   `request_hash` over the parsed fields catches a key replayed with different
   parameters and answers `409`.
4. **A failed transfer is committed, not rolled back.** Rolling back would
   release the idempotency key, so a retry could re-attempt the debit and
   succeed once the balance changed, so one key would produce two different answers.
5. **`FOR NO KEY UPDATE`, not `FOR UPDATE`.** The claim's foreign keys take a
   `FOR KEY SHARE` lock on both wallets, which `FOR UPDATE` conflicts with. Two
   concurrent transfers on one wallet would deadlock trying to upgrade a lock they
   already share, and lock ordering cannot prevent that.

---

## Layout

```
cmd/server         HTTP server
cmd/migrate        applies migrations and exits
migrations/        schema, embedded in the binary
internal/handler   thin handlers: parse, map outcomes to status codes, no logic
internal/service   business logic, orchestration, transaction boundaries
internal/repository every SQL statement; translates driver errors to domain errors
internal/domain    entities, state transitions, validation rules
internal/dbtest    fixtures shared by the integration tests
docs/design.md     the design note
```

Dependencies are one-directional:

- `domain` imports nothing.
- Nothing above `repository` imports pgx.
- All SQL lives in `repository`.

---

## Testing

```bash
docker compose up -d --wait
export TEST_DATABASE_URL='postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable'
go test -race ./...
```

Integration tests run against **real PostgreSQL**. The database is never mocked:

- A mocked query passes happily while the SQL underneath it is wrong, and the SQL
  is where the locking, the constraints and the idempotency claim actually live.
- Every bug found during this build was an interaction between layers, which no
  mock would have reproduced.

Whether they run depends on the environment:

- `TEST_DATABASE_URL` unset: the integration tests skip and the suite passes, so
  a fresh clone gives a clear message rather than a dozen connection errors.
- `CI` set: they **fail** instead. A skipped test and a passing test are both
  green, and a pipeline that quietly stopped exercising the SQL would report
  success while testing nothing.

Migrations are applied by the test fixture, so no separate step is needed.

---

## Configuration

| Variable | Used by | Default | Meaning |
|---|---|---|---|
| `DATABASE_URL` | server, migrate | — (required) | PostgreSQL connection string |
| `ADDR` | server | `:8080` | Listen address |
| `TEST_DATABASE_URL` | tests | — | Database for integration tests; unset skips them |
| `CI` | tests | — | When set, a missing `TEST_DATABASE_URL` fails instead of skipping |

Connections carry `lock_timeout = 3s`. Pessimistic locking's failure mode is one
stuck transaction blocking every transfer touching that wallet indefinitely; a
bounded wait surfaces as a retryable `503` instead of a hang.

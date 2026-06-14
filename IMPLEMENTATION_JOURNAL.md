# Implementation Journal

A chronological record of decisions made during development.
Each entry explains the _why_, not just the _what_.

---

## Phase 1 — Scaffolding

**Goal**: establish the skeleton before writing any business logic. Naming, directory layout, and migration numbering are hard to change later, so the time spent here pays compound interest.

### Directory layout decision

Chose `internal/domain`, `internal/repository`, `internal/service`, `internal/handler` over a flat structure. The `internal/` prefix prevents accidental import by external packages. Each sub-package has exactly one job.

Kept `cmd/migrate` as a separate binary from `cmd/server`. Running migrations is an operational step, not something the application server should do on startup — separating them avoids the accidental "we deployed two pods simultaneously and both tried to migrate" footgun.

### Schema decisions made at migration time

**Amounts as BIGINT**: decided upfront, before any Go code. Floating-point money arithmetic is a class of bug; storing amounts in the smallest currency unit eliminates it entirely.

**UNIQUE constraint on `transfers.idempotency_key`**: placed here rather than in a separate `idempotency_records` table. An extra table would require two round-trips on every request (insert key + insert transfer). The unique constraint on the transfer row itself achieves the same atomicity in one INSERT.

**`CHECK (balance >= 0)` on `wallets`**: the database is the last line of defence, not the first. If application logic has a defect, Postgres must still refuse to commit the invalid state.

**`CHECK (from_wallet_id <> to_wallet_id)` on `transfers`**: a same-wallet transfer is logically a no-op but would still create ledger entries, incrementing balances unnecessarily. Enforcing this at the DB level means the constraint holds even if a new code path is added that bypasses the service layer.

---

## Phase 2 — Domain Layer

**Goal**: define the core vocabulary shared by all other layers, with zero infrastructure dependencies.

### State machine design

`Transfer` exposes `MarkProcessed()` and `MarkFailed(reason)` instead of letting callers set `Status` directly. This means the only way to reach a terminal state is through the guarded methods — a direct assignment like `transfer.Status = "PROCESSED"` won't happen accidentally.

Both methods return an error if the transition is illegal (e.g., trying to process an already-failed transfer). The `ErrInvalidTransition` type carries both the source and target states, which makes debugging easier than a bare string.

### `NewLedgerPair` constructor

The double-entry invariant — every transfer creates exactly one DEBIT and one CREDIT — is enforced by giving callers a single constructor rather than individual `NewLedgerEntry` calls. It is impossible to create one without the other via this API.

### Sentinel errors

All domain errors are defined in `errors.go` as `var` sentinels. This lets callers use `errors.Is()` for matching, which continues to work through wrapping chains. The handler maps these to HTTP status codes without any string matching.

---

## Phase 3 — Repository Layer

**Goal**: isolate all SQL behind repository types. No business logic here.

### `DBTX` interface

Both `*pgxpool.Pool` and `pgx.Tx` satisfy this interface, so repository methods work inside or outside a transaction without code duplication. The service creates scoped repository instances bound to the open transaction:

```go
txWallets := repository.NewWalletRepository(tx)
```

This is simpler than passing a `tx` parameter to every method and avoids the risk of a method accidentally using the pool when it should use the transaction.

### `GetByIDForUpdate` vs `GetByID`

Two separate methods rather than a boolean parameter. A boolean parameter (`forUpdate bool`) at the call site is not self-documenting and is easy to pass the wrong value to. The method name `GetByIDForUpdate` makes the intent explicit in the calling code.

### `UpdateBalance` version check

```sql
WHERE id = $2 AND version = $3
```

If `RowsAffected()` is zero, either the wallet was deleted between the read and the update, or something else modified it despite the `FOR UPDATE` lock. Both cases are bugs; both are surfaced as errors rather than silently accepted.

---

## Phase 4 — Service Layer

**Goal**: own the transaction boundary and coordinate the repositories.

### Idempotency flow

```
INSERT transfer (claims idempotency_key)
  → unique violation → rollback → return original record   [duplicate]
  → success → continue with wallet locks                   [new request]
```

The idempotency key is claimed as the very first action inside the transaction. This means that if two identical requests arrive simultaneously, one of them will commit the key and proceed; the other will see the violation immediately, before any wallet lock is acquired, and return the original record. There is no window where both requests can proceed past this point with the same key.

### Lock ordering

```go
first, second := req.FromWalletID, req.ToWalletID
if first > second {
    first, second = second, first
}
firstWallet, _ := txWallets.GetByIDForUpdate(ctx, first)
secondWallet, _ := txWallets.GetByIDForUpdate(ctx, second)
```

Locks are acquired in ascending alphabetical order of wallet ID. Without this, a transfer from A→B and a concurrent transfer from B→A would each hold one lock and wait for the other — a deadlock. The ordering eliminates the cycle.

After acquiring locks in canonical order, the wallet references are re-mapped back to semantic `fromWallet` / `toWallet` before the balance check.

### `failTransfer`: why commit a failure?

When a transfer fails (insufficient funds, wallet not found), the service calls `tx.Commit()` rather than `tx.Rollback()`. This is deliberate:

- **Auditability**: a FAILED transfer record is persisted, so the caller can query it later. With a rollback, the record disappears.
- **Idempotency safety**: if the same request is retried (perhaps with the same idempotency key), the service returns the original FAILED record rather than re-running the transfer.
- **Client transparency**: the handler returns the failed transfer in the response body with an appropriate HTTP status. The client knows exactly what happened and why.

The only time a true rollback happens is on unexpected infrastructure errors (transaction begin failed, network error mid-flight) where there is nothing useful to commit.

---

## Phase 5 — Handler Layer

**Goal**: keep HTTP concerns out of every other layer.

### Error → HTTP status mapping

`httpStatusForError` maps domain sentinel errors to HTTP codes:

| Domain error | HTTP status | Rationale |
|---|---|---|
| Validation errors | 400 Bad Request | Malformed input |
| `ErrWalletNotFound` | 404 Not Found | Resource doesn't exist |
| `ErrInsufficientFunds` | 422 Unprocessable Entity | Request is well-formed but cannot be fulfilled |
| Other errors | 500 Internal Server Error | Unexpected; don't leak details |

`422 Unprocessable Entity` is preferred over `400` for insufficient funds because the request itself is valid — it's the business state that prevents execution.

### Response body for failed transfers

When the service returns `(transfer, error)` with a non-nil transfer (i.e. a FAILED transfer was persisted), the handler writes the transfer body alongside the error status code. This lets clients inspect the recorded failure reason without making a second request.

### Graceful shutdown

The server registers `SIGINT`/`SIGTERM` handlers and drains in-flight requests for up to 15 seconds before exiting. Without this, a deploy or container stop would abruptly close connections in the middle of transfers.

---

## Phase 6 — Testing

**Goal**: test behaviour, not implementation details.

### Real database, no mocks

All integration tests run against a real Postgres instance. Mocking the database would defeat the purpose of testing the concurrency and idempotency strategies — those properties are only meaningful when real transactions are involved.

The `testutil.NewTestPool` helper skips the test gracefully (`t.Skip`) if the database is unreachable, so the domain unit tests can still run in CI environments without Postgres.

### Test isolation

Each test gets a fresh slate via `truncateAll` in a `t.Cleanup` function. Tables are truncated in dependency order (ledger_entries → transfers → wallets) so foreign-key constraints are not violated during cleanup.

### Concurrency test design

```go
for i := range workers {
    go func(n int) {
        svc.CreateTransfer(ctx, TransferRequest{
            IdempotencyKey: fmt.Sprintf("concurrent-%d", n),
            ...
        })
    }(i)
}
wg.Wait()

assert.Equal(t, initialBalance, sourceBalance + sinkBalance)
assert.Equal(t, int64(successes) * transferAmount, sinkBalance)
```

Twenty goroutines, each with a unique idempotency key, debit the same source wallet simultaneously. The test does not assert how many succeed (some may fail with insufficient funds if they race past others), but it asserts:

1. **Money conservation**: total value in the system never changes.
2. **Accounting consistency**: the sink balance exactly equals the number of successful transfers multiplied by the transfer amount.

This is a stronger assertion than checking that a specific number of transfers succeeded — it catches double-spend and partial-update bugs regardless of scheduling order.

---

## What Would Come Next (Given More Time)

- **Transfer history API**: `GET /wallets/{id}/transfers` listing ledger entries by wallet.
- **Metrics**: Prometheus counters for transfer throughput, failure rate, and latency histogram.
- **Structured error codes**: add a machine-readable `code` field to error responses alongside the human `error` string so clients can act programmatically on specific failure types.
- **Pagination**: the ledger entry query currently returns all rows; a cursor-based approach would be needed at scale.
- **`wallet_test` creation in CI**: currently manual; could be automated with a `psql` step that checks-or-creates.

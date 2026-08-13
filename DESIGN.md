# Design Note — Wallet Transfer Service

Written before implementation, per the assignment's documentation-first workflow.

## 1. Problem Statement

Provide an HTTP API that moves money between two wallets, guaranteeing:
exactly-once processing per `idempotencyKey`, a balanced double-entry ledger,
correct balances under concurrent transfers, and safe state transitions.

## 2. API Contract

### `POST /transfers`

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

`amount` is an integer in the wallet's smallest currency unit (e.g. cents) —
no floats, to avoid rounding errors in money math.

Responses:

| Case                                   | HTTP | Body                                   |
|-----------------------------------------|------|-----------------------------------------|
| First successful processing             | 201  | transfer resource, `status=PROCESSED`   |
| Replay of a completed key               | 200  | the original stored response, verbatim  |
| Same key, in-flight (another goroutine) | 409  | `request already in progress`           |
| Same key, different payload             | 409  | `idempotency key reused with a different payload` |
| Insufficient funds                      | 422  | transfer resource, `status=FAILED`      |
| Wallet not found                        | 404  | error                                   |
| Currency mismatch between wallets       | 400  | error                                   |
| Validation error (bad amount, same wallet on both sides, missing key) | 400 | error |

### Supporting endpoints (optional enhancements, included for testability)

- `POST /wallets` — create a wallet with an opening balance (needed because the
  assignment has no wallet-provisioning endpoint and we need a way to seed data).
- `GET /wallets/:id` — current balance.
- `GET /transfers/:id` — transfer status.
- `GET /wallets/:id/ledger` — ledger entries for a wallet (transfer history).
- `GET /healthz` — liveness.

## 3. Data Model

```
wallets            (id PK, balance, currency, timestamps)
transfers          (id PK, idempotency_key, from_wallet_id FK, to_wallet_id FK,
                     amount, status, failure_reason, timestamps)
ledger_entries     (id PK, transfer_id FK, wallet_id FK, entry_type, amount, created_at)
idempotency_records(idempotency_key PK, request_hash, status, transfer_id FK,
                     timestamps)
```

Key constraints:

- `wallets.balance >= 0` (CHECK) — the last line of defense against double-spend.
- `transfers.amount > 0` and `from_wallet_id <> to_wallet_id` (CHECK).
- `ledger_entries.amount > 0`, `entry_type IN ('DEBIT','CREDIT')`, `transfer_id NOT NULL FK`
  — a ledger row can never exist without a parent transfer.
- `idempotency_records.idempotency_key` is the PRIMARY KEY — the uniqueness
  guarantee that makes idempotency actually safe under concurrent duplicate
  requests (an application-level "check then insert" without this constraint
  is a race condition, not a guarantee).
- Ledger rows are only ever written for `PROCESSED` transfers. A `FAILED`
  transfer has zero ledger entries — nothing moved, nothing to record.

## 4. Idempotency Strategy

Storage: `idempotency_records`, keyed on the client-supplied `idempotencyKey`,
durable in Postgres (safe across process restarts/crashes — nothing is kept
in memory).

Flow:

1. Hash the request body — `sha256` of a JSON-encoded `{from, to, amount}`
   struct, not a delimiter-joined string. Wallet IDs are unconstrained, so a
   plain `fromWalletId|toWalletId|amount` join is ambiguous: `("a|b","c",100)`
   and `("a","b|c",100)` both produce `"a|b|c|100"`. JSON's string escaping
   makes field boundaries unambiguous regardless of what a wallet ID
   contains. This catches a client reusing the same key for a *different*
   request, which is a bug on the caller's side, not a legitimate retry.
2. `INSERT ... ON CONFLICT (idempotency_key) DO NOTHING`. If the insert lands,
   this request owns the key and proceeds to execute the transfer.
3. If the insert conflicts, an idempotency record already exists:
   - `status = COMPLETED` and hash matches → **replay**: re-fetch the transfer
     the key is linked to (`transfer_id`) and return it. No separate response
     snapshot is stored — the transfer row already written to the DB *is*
     the durable source of truth, so replay is just "read it back". This
     also sidesteps response-schema drift between a stored blob and the
     current API shape.
   - hash does not match → `409`, key reuse with a different payload.
   - `status = PENDING` → another request with the same key is (or recently
     was) in flight → `409`, ask the caller to retry or check the transfer
     status.
4. On completion (success or business failure like insufficient funds), the
   idempotency record is updated to `COMPLETED` and linked to the transfer id,
   **in the same DB transaction** as the transfer/ledger/balance writes. So
   either the whole thing lands atomically, or none of it does. If a
   validation failure happens *before* any transfer can legally exist (e.g.
   the wallet doesn't exist, so there's nothing valid to write), the claim is
   released (`DELETE ... WHERE status='PENDING'`) instead of completed,
   freeing the key for a future retry rather than wedging it on a response
   that was never durably recorded.
5. **Commit outcome is not always knowable.** If the final `COMMIT` itself
   fails (e.g. the connection drops after the command is sent but before the
   acknowledgement comes back), the transaction may have actually landed on
   the server even though the client saw an error — unlike a failed statement
   earlier in the transaction, this case can't be treated as "definitely
   nothing was written." `WithinTx` wraps this specific failure in
   `domain.ErrCommitOutcomeUnknown`, and `CreateTransfer` checks for it
   explicitly: on any other error the key is released for retry as described
   above, but on this one it is deliberately left `PENDING` rather than
   released, so a retry gets `409` instead of risking a second transfer
   stacked on top of a commit that already succeeded.

**Deliberately not implemented: automatic lease-based reclaim of a stuck
`PENDING` key.** The tempting alternative is "if a `PENDING` record is older
than N seconds, assume its owner crashed and let a new request take over."
That is unsafe without a fencing token: the original request might just be
slow, not dead, and could still call `Complete` after a reclaimer has already
started reprocessing — producing two transfers for one key. Given the
assignment's time budget, the safer choice is to surface `409` for a stuck
key and let the caller decide (retry the same key later, or check
`GET /transfers/:id` / retry with a new key) rather than silently risk a
double-spend. A production version of this would add a monotonic `attempt`
column and require `Complete` to CAS on it, or move key ownership to a
dedicated lock service — noted here as a known gap, not solved.

## 5. Concurrency Strategy

Two transfers touching the same wallet at the same time must not both read
the same starting balance and both "succeed" against it (lost update /
double-spend).

Chosen approach: **explicit row locks inside a DB transaction**
(`SELECT ... FOR UPDATE` on both wallets), not optimistic locking and not
`SERIALIZABLE` isolation.

- `SELECT ... FOR UPDATE` blocks a second transaction from reading the row
  for update until the first commits — the second transaction's balance
  check sees the *post-transfer* balance, not a stale one. This directly
  prevents the "two transfers debit the same wallet simultaneously" case
  from the assignment.
- Wallets are always locked in a **deterministic global order** (ascending
  wallet ID), regardless of which one is the source and which is the
  destination. Without this, transfer A→B and a concurrent transfer B→A
  can each hold one lock and wait on the other — a classic deadlock. Locking
  in a fixed order makes that impossible.
- Rejected `SERIALIZABLE` isolation because it pushes the burden onto retry
  loops (serialization failures under contention need application-level
  retry) for no benefit over explicit locks here — the lock scope is small
  (two rows) and well understood, so pessimistic locking is simpler to
  reason about and test.
- The whole unit — lock both wallets, verify sufficient funds, insert the
  transfer row, insert both ledger rows, update both balances, mark the
  transfer `PROCESSED`, complete the idempotency record — happens inside one
  `BEGIN...COMMIT`. If any step fails, everything rolls back, so a half
  -applied transfer (e.g. debited but never credited) is not reachable.
- `wallets.balance >= 0` as a DB CHECK constraint is the final backstop: even
  if application logic had a bug, the database itself refuses to let a
  balance go negative.

## 6. Transfer State Machine

```
PENDING -> PROCESSED   (funds moved, 2 ledger rows written)
PENDING -> FAILED      (e.g. insufficient funds; 0 ledger rows written)
```

Both transitions happen inside the same transaction that creates the
transfer row, so a transfer is only ever observable in a terminal state —
there's no window where a client can `GET` a transfer and see `PENDING`
followed by it changing later (this is a synchronous API, not an async
workflow). `PENDING` exists as an explicit state to represent the internal
in-progress step and to leave room for an async/queued execution model
later without changing the schema. Terminal states never transition again —
enforced in the domain layer.

## 7. Failure Modes Considered

| Scenario | Handling |
|---|---|
| Duplicate request (same key, network retry) | Replayed from `idempotency_records`, no duplicate side effects |
| Same key, concurrent in-flight duplicate | `409`, second caller retries |
| Crash after claiming idempotency key, before finishing | Key stays `PENDING`; `409` on any retry until manually resolved — see §4's note on why auto-reclaim was deliberately not implemented |
| Commit ack lost (connection drops right after `COMMIT`, outcome unknown) | `domain.ErrCommitOutcomeUnknown`; key left `PENDING` rather than released — see §4 point 5 |
| Caller's request context cancelled/times out after the transfer logic succeeded but before commit | Commit runs on its own short-lived context (like rollback already did), so it isn't aborted by a cancellation that has nothing to do with whether the work itself succeeded |
| Concurrent transfers on same wallet | Row-level locks in deterministic order |
| Insufficient balance | Transfer recorded as `FAILED`, no ledger rows, balances untouched |
| Wallet does not exist | `404`, nothing written, idempotency key released for retry |
| Currency mismatch between wallets | `400`, nothing written, idempotency key released for retry |
| Partial failure mid-transaction (e.g. DB error after debit) | Whole DB transaction rolls back — nothing partially applied |

## 8. Testing Strategy

- **Unit tests** (service layer, in-memory fake repositories): idempotency
  replay, hash-mismatch rejection, insufficient-funds path, state-transition
  rules, validation errors. Fast, no DB required.
- **Integration tests** (real Postgres via `docker-compose`, build-tag gated):
  schema constraints, actual row locking, and a genuine concurrency test that
  fires N goroutines transferring out of the same wallet simultaneously and
  asserts the final balance and ledger are consistent (no lost updates).

## 9. Observability

Structured request logging (method, path, status, latency) via Gin middleware,
plus a `/healthz` endpoint. Metrics/tracing are out of scope for the 3-5 hour
budget and are listed as optional enhancements in `ASSIGNMENT.md`.

## 10. Assumptions / Tradeoffs

- No cross-currency conversion: a transfer requires `from.Currency ==
  to.Currency`, rejected with `ErrCurrencyMismatch` (400) otherwise. Real
  multi-currency support would need an FX-rate service and is out of scope.
- No auth/authz layer — out of scope for this assignment.
- Wallet creation endpoint added even though not in the spec, purely so the
  system is runnable/testable end to end.

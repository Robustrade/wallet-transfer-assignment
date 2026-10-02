# Wallet Transfer Service: Design Note

Written before the implementation, as the assignment asks. Update this file when a decision changes.

## 1. Problem statement

`POST /transfers` moves money between two wallets. It must be safe under:

- **duplicate requests / retries** (idempotency)
- **concurrent requests on the same wallet** (no double spend)
- **crashes mid-way** (atomicity, ledger and balances never disagree)

## 2. Assumptions and scope

- Single currency. `amount` is an **integer in minor units** (paise/cents), stored as `BIGINT`. No floats.
- No authentication. The caller is assumed trusted and entitled to debit `fromWalletId`.
- Wallets already exist. They are created by a dev/test seed script, not by the API. Opening balances have **no ledger rows**, so the reconciliation invariant is:
  `balance = opening_balance + sum(CREDIT) - sum(DEBIT)`.
- PostgreSQL 18 (`uuidv7()` is built in).

## 3. API contract

### `POST /transfers`

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "<uuid>",
  "toWalletId": "<uuid>",
  "amount": 100
}
```

| Situation | HTTP | `error` code |
|---|---|---|
| Transfer created | `201` | |
| Replay, same key and same payload, original succeeded | `200` + header `Idempotent-Replayed: true`, identical body | |
| Replay, same key and same payload, original failed | the stored code (`422`) and body | `INSUFFICIENT_FUNDS` |
| Malformed body, `amount <= 0`, `from == to`, empty key | `400` | `VALIDATION_ERROR` |
| A wallet does not exist | `404` | `WALLET_NOT_FOUND` |
| Same key, different payload | `422` | `IDEMPOTENCY_KEY_REUSED` |
| Insufficient funds | `422` | `INSUFFICIENT_FUNDS` |
| Key older than the retention window | `409` | `IDEMPOTENCY_KEY_EXPIRED` |
| Technical failure (DB down, timeout) | `500`/`503`, **nothing stored** | `INTERNAL` |

Two cases share `422`, so clients branch on the `error` field, not the status.

## 4. Data model

See `migrations/0001_init.up.sql`. Summary:

- `wallets(id, owner_id, balance BIGINT CHECK >= 0, ...)`
- `transfers(id, idempotency_key, from_wallet_id, to_wallet_id, amount CHECK > 0, state, failure_reason, ...)`
  - `UNIQUE (from_wallet_id, idempotency_key)`: permanent backstop against double execution
  - `CHECK (from <> to)`, `CHECK ((state='FAILED') = (failure_reason IS NOT NULL))`
- `ledger_entries(id, wallet_id, transfer_id, type, amount CHECK > 0, ...)`
  - `UNIQUE (transfer_id, type)`: at most one DEBIT and one CREDIT per transfer
  - insert-only (the repository exposes no UPDATE or DELETE)
- `idempotency_records(wallet_id, key, request_hash, status, transfer_id, response_code, response_body, retain_until)`
  - `PRIMARY KEY (wallet_id, key)`: keys are scoped to the source wallet

Indexes: 
- `ledger(wallet_id, created_at)`, 
- `ledger(transfer_id)`,
- `transfers(from_wallet_id, created_at)`,
- `transfers(to_wallet_id, created_at)`,
- `idempotency_records(retain_until)`.

## 5. Transfer workflow (one DB transaction)

1. Validate input (no DB access). On failure return `400`.
2. `BEGIN` (default `READ COMMITTED`).
3. Lock both wallets: `SELECT id, balance FROM wallets WHERE id IN ($from,$to) ORDER BY id FOR UPDATE`.
   - Fewer than two rows returned: `404`.
4. Look up `idempotency_records` by `(from, key)`.
   - Found and not past `retain_until`:
     - `request_hash` differs: `422 IDEMPOTENCY_KEY_REUSED`.
     - Same hash: replay the stored response.
   - Found but past `retain_until` (purger has not run yet): treat the same as purged, see below.
   - Not found: check `transfers` for `(from, key)`. If it exists, the key is expired: `409`.
5. Insert the transfer as `PENDING`.
6. If `source.balance < amount`:
   - guarded update `PENDING -> FAILED` with `failure_reason = 'INSUFFICIENT_FUNDS'`
   - store the idempotency record with the `422` response
   - `COMMIT` (no balances changed)
7. Otherwise:
   - `UPDATE wallets SET balance = balance - $amount WHERE id = $from`
   - `UPDATE wallets SET balance = balance + $amount WHERE id = $to`
   - insert two ledger rows (DEBIT on source, CREDIT on target)
   - guarded update `PENDING -> PROCESSED`
   - store the idempotency record with the `201` response
   - `COMMIT`
8. Any technical error: `ROLLBACK`, store nothing, return `5xx`. The client retries with the same key.

`PENDING` exists in the domain model and inside the transaction, but is never visible in committed data (see "Tradeoffs").

## 6. Concurrency strategy

**Pessimistic row locking with `SELECT ... FOR UPDATE`, at the default `READ COMMITTED` isolation.**

- Without locks, two transactions can both read balance 100, both decide 80 is affordable, and both debit. `FOR UPDATE` makes the second transaction wait on its `SELECT`, and when it resumes it reads the committed balance (20) and fails the check.
- `SERIALIZABLE` would also work, but it needs retry loops for serialization failures. Row locks are simpler to reason about for a two-row operation.
- **Deadlock avoidance:** wallets are always locked in ascending id order, in a single statement with `ORDER BY id`. A->B and B->A both lock the lower id first, so one waits instead of deadlocking.
- **Last line of defense:** `CHECK (balance >= 0)` makes a double spend fail in the database even if the application logic is wrong.
- **Same key arriving twice at once:** both requests take the wallet locks first, in the same order. The second waits, then finds the stored record and replays it. The unique constraints on `idempotency_records` and `transfers` are the backstop.

## 7. Idempotency strategy

- **Storage:** `idempotency_records` keyed by `(source wallet, client key)`.
- **Duplicate detection:** lookup under the wallet lock, then compare `request_hash` = `sha256(lower(from) | lower(to) | amount)`. The key itself is not part of the hash.
- **Returning the original result:** the stored `response_code` and `response_body` are returned verbatim. The incoming payload is only used for the hash comparison, so a different payload never receives the old response.
- **Preventing duplicate side effects:** the idempotency record, the transfer, the balances and the ledger rows commit in one transaction, so there is no state where money moved but the key is unrecorded.
- **Outcome is fixed once decided.** A key that failed with `INSUFFICIENT_FUNDS` always replays that failure, even after a top-up. To retry, the client sends a new key.
- **Retention:** `IDEMPOTENCY_RETENTION` (default `24h`, `1m` for testing). `retain_until` is written by the app and also checked on lookup, so an expired key behaves the same whether or not the purger has run. After expiry the permanent `UNIQUE (from_wallet_id, idempotency_key)` on `transfers` blocks re-execution and the API returns `409`.
- **Purger:** a separate container (`cmd/purger`) deletes expired `idempotency_records` in batches. It never touches `transfers` or `ledger_entries`.

## 8. Failure modes

| Failure | Effect | Client action |
|---|---|---|
| Crash before commit | Nothing stored | Retry with the same key |
| Crash after commit, response lost | Everything stored | Retry with the same key and get the stored response |
| Two identical requests at once | One executes, the other waits and replays | None |
| Same key, different payload | `422`, nothing executed | Use a new key |
| Insufficient funds | `FAILED` stored, replayed identically | Send a new key |
| Key retried after the retention window | `409` | Check the transfer state out of band, or use a new key |
| Deadlock | Prevented by lock ordering | n/a |

## 9. Observability

Structured logs (`log/slog`) with `transfer_id`, `idempotency_key`, `from`, `to`, `amount`, `state_transition`, and `replayed=true|false` on every request. Metrics are out of scope.

## 10. Layering

- `internal/handler`: gin routes, request validation, transport mapping, thin
- `internal/service`: workflow, idempotency decisions, transaction orchestration
- `internal/repository`: SQL only, takes a `pgx.Tx`
- `internal/domain`: `Wallet`, `Transfer` with the state machine, errors, hashing
- `cmd/app`, `cmd/purger`; migrations run via the `migrate/migrate` image, no custom code

## 11. Testing strategy (red, then green, then refactor)

Behavioral tests against a real Postgres 18 (testcontainers):

1. Transfer success: balances move, two ledger rows, transfer `PROCESSED`.
2. Insufficient funds: `422`, balances unchanged, no ledger rows, replay returns the same `422` even after a top-up.
3. Replay: same key and payload returns `200`, identical body, no second transfer.
4. Key reuse with a different payload: `422 IDEMPOTENCY_KEY_REUSED`, nothing executed.
5. **Concurrent debits** on one wallet: N goroutines, final balance correct, never negative.
6. **Same key fired concurrently:** exactly one transfer and one pair of ledger rows.
7. **Opposing transfers** (A->B and B->A concurrently): no deadlock.
8. Forced failure mid-transaction: balances unchanged, no orphan ledger rows.
9. Expired key: `409`, no second transfer.
10. **Ledger invariant** after all of the above: total debits equal total credits, and each wallet's `balance = opening + credits - debits`.
11. Domain unit tests for the state machine.

## 12. Tradeoffs

- **Single transaction (no committed PENDING).** Simpler, with no stuck-PENDING recovery. The cost is that `PENDING` is never externally visible, so the state machine lives in the domain model and the guarded updates.
- **Business failures are stored.** Same key always gives the same answer, at the cost of a `transfers` row for each failed attempt.
- **Locking before the idempotency lookup** serializes same-source requests briefly. Fine at this scale.
- **Retention is config-driven.** The purger is a separate process so the API carries no cleanup code.

## 13. AI usage

To be filled in at PR time: tool, how it was used, and the full session transcript (see the repo's `docs/ai-transcript.md`).
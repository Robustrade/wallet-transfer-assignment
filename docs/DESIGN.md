# Wallet Transfer Service: Design Note

Written before the implementation, as the assignment asks. Update this file when a decision changes.

## 1. Problem statement

`POST /transfers` moves money between two wallets. It must be safe under:

- **duplicate requests / retries** (idempotency)
- **concurrent requests on the same wallet** (no double spend)
- **crashes mid-way** (atomicity, ledger and balances never disagree)

## 2. Assumptions and scope

- Single currency. `amount` is an **integer in minor units** (paise/cents), stored as `BIGINT`. No floats.
- No authentication, so there is no caller or client identity. The caller is assumed trusted and entitled to debit `fromWalletId`.
- Wallets already exist. They are created by a dev/test seed script, not by the API, and are never deleted (foreign keys from `transfers` and `ledger_entries` enforce this). Opening balances have **no ledger rows**, so the reconciliation invariant is:
  `balance = opening_balance + sum(CREDIT) - sum(DEBIT)`.
- The idempotency key is an opaque client string, expected to be high-entropy (for example a UUID).
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
| Transfer created | `200` | |
| Replay of a successful transfer (same key, same payload) | `200`, identical body, header `Idempotent-Replayed: true` | |
| Replay of a failed transfer (same key, same payload) | the stored `422` and body, header `Idempotent-Replayed: true` | `INSUFFICIENT_FUNDS` |
| Malformed body, `amount <= 0`, `from == to`, empty key | `400` | `VALIDATION_ERROR` |
| Same key, different payload (any of from, to, amount) | `422` | `IDEMPOTENCY_KEY_REUSED` |
| Key has no record, and a wallet does not exist | `404` | `WALLET_NOT_FOUND` |
| Insufficient funds | `422` | `INSUFFICIENT_FUNDS` |
| Key older than the retention window | `409` | `IDEMPOTENCY_KEY_EXPIRED` |
| Technical failure (DB down, timeout) | `500`/`503`, **nothing stored** | `INTERNAL` |

- **Status is deterministic.** A new success and its replays all return `200`. The only thing that tells them apart is the `Idempotent-Replayed` header. The stored `response_code` is `200` for a success and `422` for insufficient funds, and a replay returns the stored code and body verbatim.
- Two cases share `422`, so clients branch on the `error` field, not the status.
- **Precedence:** validation (`400`) is checked first because it needs no state. After that, the idempotency record decides, and only a key with no record reaches wallet-dependent errors (`404`, insufficient funds). So a retried key can never turn into a `404`.

## 4. Data model

See `migrations/0001_init.up.sql`. Summary:

- `wallets(id, owner_id, balance BIGINT CHECK >= 0, ...)`
- `transfers(id, idempotency_key, from_wallet_id, to_wallet_id, amount CHECK > 0, state, failure_reason, ...)`
  - `UNIQUE (idempotency_key)`: permanent, global backstop against double execution
  - `CHECK (from <> to)`, `CHECK ((state='FAILED') = (failure_reason IS NOT NULL))`
- `ledger_entries(id, wallet_id, transfer_id, type, amount CHECK > 0, ...)`
  - `UNIQUE (transfer_id, type)`: at most one DEBIT and one CREDIT per transfer
  - insert-only (the repository exposes no UPDATE or DELETE)
- `idempotency_records(key, request_hash, status, transfer_id, response_code, response_body, retain_until)`
  - `PRIMARY KEY (key)`: the key namespace is global and independent of the payload

Indexes: `ledger(wallet_id, created_at)`, `ledger(transfer_id)`, `transfers(from_wallet_id, created_at)`, `transfers(to_wallet_id, created_at)`, `idempotency_records(retain_until)`.

**Why the key is global, not scoped to the source wallet.** The key's namespace must not depend on anything in the payload. If the key were scoped by `fromWalletId`, reusing it with a different source would miss the lookup and execute a second transfer instead of returning `IDEMPOTENCY_KEY_REUSED`. With no authentication there is no client id to namespace by. If auth is added, the namespace becomes `(client_id, key)`, still independent of the payload.

## 5. Transfer workflow (one DB transaction)

1. Validate input (no DB access). On failure return `400`.
2. `BEGIN` (default `READ COMMITTED`).
3. **Serialize on the key:** `SELECT pg_advisory_xact_lock(hashtextextended($key, 0))`. It is released automatically at commit or rollback.
4. **Look up the idempotency record by key (before any wallet check):**
   - Found and not past `retain_until`:
     - `request_hash` differs: `422 IDEMPOTENCY_KEY_REUSED`.
     - Same hash: replay the stored response (`Replayed = true`).
   - Found but past `retain_until` (purger has not run yet): `409 IDEMPOTENCY_KEY_EXPIRED`.
   - Not found, but a `transfers` row exists with this key (record purged): `409 IDEMPOTENCY_KEY_EXPIRED`.
5. Lock both wallets: `SELECT id, balance FROM wallets WHERE id IN ($from,$to) ORDER BY id FOR UPDATE`.
   - Fewer than two rows returned: `404`. Nothing is stored for this key.
6. Insert the transfer as `PENDING`.
7. If `source.balance < amount`:
   - guarded update `PENDING -> FAILED` with `failure_reason = 'INSUFFICIENT_FUNDS'`
   - store the idempotency record with the `422` response
   - `COMMIT` (no balances changed)
8. Otherwise:
   - `UPDATE wallets SET balance = balance - $amount WHERE id = $from`
   - `UPDATE wallets SET balance = balance + $amount WHERE id = $to`
   - insert two ledger rows (DEBIT on source, CREDIT on target)
   - guarded update `PENDING -> PROCESSED`
   - store the idempotency record with the `200` response
   - `COMMIT`
9. Any technical error: `ROLLBACK`, store nothing, return `5xx`. The client retries with the same key.

`PENDING` exists in the domain model and inside the transaction, but is never visible in committed data (see "Tradeoffs").

## 6. Concurrency strategy

**A per-key advisory lock plus pessimistic row locking with `SELECT ... FOR UPDATE`, at the default `READ COMMITTED` isolation.**

- Without locks, two transactions can both read balance 100, both decide 80 is affordable, and both debit. `FOR UPDATE` makes the second transaction wait on its `SELECT`, and when it resumes it reads the committed balance (20) and fails the check.
- `SERIALIZABLE` would also work, but it needs retry loops for serialization failures. Row locks are simpler to reason about for a two-row operation.
- **Same key arriving twice at once:** the advisory lock (step 3) is taken before the idempotency lookup, so the lookup itself is the serialized check. The second request blocks on the lock, then runs the lookup after the first has committed and sees its record. There is no unlocked "first look" that could race, so no separate recheck is needed. This holds even when the two requests name different wallets (and therefore share no wallet lock): the loser gets `IDEMPOTENCY_KEY_REUSED`, not a unique-violation error.
- **Deadlock avoidance:** every transaction takes locks in the same order: the key advisory lock first, then the wallets in ascending id order, in a single statement with `ORDER BY id`. A transaction holds at most one advisory lock, and always takes it before any wallet lock, so there is no cycle. A->B and B->A both lock the lower wallet id first, so one waits instead of deadlocking.
- **Hash collisions in `hashtextextended`** only cause two different keys to serialize briefly. They never affect correctness.
- **Last line of defense:** `CHECK (balance >= 0)` makes a double spend fail in the database even if the application logic is wrong. `PRIMARY KEY (key)` on `idempotency_records` and `UNIQUE (idempotency_key)` on `transfers` make a second execution of the same key fail even if the locking were wrong.

## 7. Idempotency strategy

- **Storage:** `idempotency_records` keyed by the client key alone (global namespace).
- **Duplicate detection:** lookup by key under the key lock, then compare `request_hash` = `sha256(lower(from) | lower(to) | amount)`. The key itself is not part of the hash. Because the source wallet is in the hash, reusing a key with a different source is a different payload and returns `IDEMPOTENCY_KEY_REUSED`.
- **Returning the original result:** the stored `response_code` and `response_body` are returned verbatim, with the `Idempotent-Replayed` header added. The incoming payload is only used for the hash comparison, so a different payload never receives the old response.
- **Replays never depend on wallet state.** The record is checked before wallets are looked up, so neither a changed balance nor a missing wallet can change a replay. (Wallets are never deleted in this design, but the order does not rely on that.)
- **Preventing duplicate side effects:** the idempotency record, the transfer, the balances and the ledger rows commit in one transaction, so there is no state where money moved but the key is unrecorded.
- **Outcome is fixed once decided.** A key that failed with `INSUFFICIENT_FUNDS` always replays that failure, even after a top-up. To retry, the client sends a new key. A `404` is not stored, because no transfer was decided.
- **Retention:** `IDEMPOTENCY_RETENTION` (default `24h`, `1m` for testing). `retain_until` is written by the app and also checked on lookup, so an expired key behaves the same whether or not the purger has run. After expiry the permanent `UNIQUE (idempotency_key)` on `transfers` blocks re-execution and the API returns `409`.
- **Purger:** a separate container (`cmd/purger`) deletes expired `idempotency_records` in batches. It never touches `transfers` or `ledger_entries`.

## 8. Failure modes

| Failure | Effect | Client action |
|---|---|---|
| Crash before commit | Nothing stored | Retry with the same key |
| Crash after commit, response lost | Everything stored | Retry with the same key and get the stored response |
| Two identical requests at once | One executes, the other waits and replays | None |
| Same key, different payload (including a different source, or a nonexistent destination) | `422 IDEMPOTENCY_KEY_REUSED`, nothing executed | Use a new key |
| Same key from two different sources at once | One executes, the other gets `422 IDEMPOTENCY_KEY_REUSED` | Use a new key |
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

Behavioral tests against a real Postgres 18 (the compose database, via `TEST_DATABASE_URL`):

1. Transfer success: `200`, balances move, two ledger rows, transfer `PROCESSED`.
2. Insufficient funds: `422`, balances unchanged, no ledger rows, replay returns the same `422` (with the replay flag) even after a top-up, and a new key succeeds.
3. Replay: same key and payload returns `200` with the replay flag, identical body, no second transfer.
4. Key reuse with a different payload (amount, target, **source**, or a **nonexistent destination**): `422 IDEMPOTENCY_KEY_REUSED`, never `404`, nothing executed.
5. **Concurrent debits** on one wallet: N goroutines, final balance correct, never negative.
6. **Same key fired concurrently:** exactly one transfer and one pair of ledger rows.
7. **Same key fired concurrently from different sources:** exactly one executes, the rest get `IDEMPOTENCY_KEY_REUSED`, and no technical error.
8. **Opposing transfers** (A->B and B->A concurrently): no deadlock.
9. Forced failure mid-transaction: balances unchanged, no orphan ledger rows, and the same key works afterwards.
10. Expired key: `409`, no second transfer, whether or not the record was purged.
11. **Ledger invariant** after all of the above: total debits equal total credits, and each wallet's `balance = opening + credits - debits`.
12. Domain unit tests for the state machine and the request hash.

Not tested: "an exact replay after a wallet disappears", because foreign keys prevent deleting a wallet that has transfers. The record-first ordering in section 5 is what covers it.

## 12. Tradeoffs

- **Single transaction (no committed PENDING).** Simpler, with no stuck-PENDING recovery. The cost is that `PENDING` is never externally visible, so the state machine lives in the domain model and the guarded updates.
- **Business failures are stored.** Same key always gives the same answer, at the cost of a `transfers` row for each failed attempt.
- **Global key namespace.** Without authentication, one caller's key can collide with another's. That produces `422` or `409` rather than a wrong transfer, and high-entropy keys make it unlikely. With auth, the namespace would be `(client_id, key)`.
- **Per-key advisory lock** serializes only requests that share a key, which is the only case that needs it. It costs one extra statement per request.
- **Retention is config-driven.** The purger is a separate process so the API carries no cleanup code.

## 13. AI usage

To be filled in at PR time: tool, how it was used, and the full session transcript (see the repo's `docs/ai-transcript.md`).
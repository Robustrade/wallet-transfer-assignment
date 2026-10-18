# Wallet Transfer Service — API Contract

**Status:** Implemented contract (aligned with the code)  
**Stack:** Go, Fiber v3, GORM, PostgreSQL  
**Version:** v1

## 1. Purpose

This document specifies HTTP request/response behavior for wallet-to-wallet transfers. It separates assignment-mandated behavior from explicit implementation decisions. The service provides atomic wallet transfers, balanced double-entry ledger records, retry-safe idempotency, and concurrency-safe balances.

## 2. General conventions

- Base path: `/` (no version prefix required by the assignment).
- JSON request and response bodies (`Content-Type: application/json`).
- Amount is a **positive integer in the system's smallest supported currency unit** (`int64`); no floating-point arithmetic. Currency and denomination must be agreed before deployment. For the sample contract, `100` means 100 smallest units, **not necessarily ₹100**.
- **Wallet IDs are positive integers** (`int64`, database identity values). Transfer IDs are positive integers as well.
- **Idempotency keys are strings of 8–64 characters.** The handler and domain reject other lengths with `400`; the database enforces the same range (`chk_idempotency_key_length`) as a backstop.
- An idempotency key identifies one logical request. A new intended transfer requires a new key.
- Error responses use the envelope `{"error": "<message>"}`, including errors raised by the framework (unknown route `404 {"error":"Not Found"}`, wrong method `405`). Internal error details are never returned.
- No authentication scheme is prescribed by the assignment. Production authentication and idempotency-key scoping require a separate decision.

## 3. POST /transfers (required)

Create an atomic wallet transfer.

### Request

```http
POST /transfers HTTP/1.1
Content-Type: application/json
```

```json
{
  "idempotencyKey": "abc12345",
  "fromWalletId": 1,
  "toWalletId": 2,
  "amount": 100
}
```

### Validation

- All four fields are required. `fromWalletId`, `toWalletId` and `amount` must be integers greater than zero.
- `idempotencyKey` must not be blank and must be 8–64 characters.
- Source and destination wallet IDs must differ.
- Both wallets must exist; this is checked under the row lock (see §5).
- Malformed JSON and fields of the wrong type (for example a string wallet ID) are rejected with `400`. Unknown extra fields are ignored.
- The server computes a canonical SHA-256 fingerprint from `fromWalletId`, `toWalletId` and `amount`. The idempotency key is not part of that fingerprint.

### Successful transfer — `201 Created`

```json
{
  "status": "PROCESSED",
  "transferId": 1001
}
```

The source wallet is debited, the destination wallet credited, and exactly two balanced ledger entries are committed in the same PostgreSQL transaction as the transfer's final state and saved idempotency result.

### Business failure — `409 Conflict`

For insufficient funds, a terminal `FAILED` transfer (`failure_reason = INSUFFICIENT_FUNDS`) and its response are committed without balance changes or ledger entries.

```json
{
  "error": "insufficient funds"
}
```

The assignment does not prescribe a particular HTTP status for business failures; `409` is this project's choice.

### Wallet not found — `404 Not Found`

```json
{
  "error": "wallet not found"
}
```

Either wallet missing rolls back the whole transaction. No transfer is persisted and the idempotency key is **not** reserved, so the same key can be reused once the wallet exists.

### Same key, same payload — replay original outcome

When the key has already committed, return the **original HTTP status and response body** (including a recorded `409` insufficient-funds outcome) without any new balance or ledger changes. This includes retry after a successful commit whose HTTP response was lost.

The stored body is `JSONB`, which PostgreSQL re-formats. The service re-serialises it to the canonical compact form, so a replay is **byte-identical** to the original response (transport headers such as `Date` may differ).

### Same key, different payload — `409 Conflict`

```json
{
  "error": "idempotency key already used with different request"
}
```

No new transfer or ledger entries are created.

### Same key, original request still in progress — `409 Conflict`

```json
{
  "error": "request with this idempotency key is still in progress; retry shortly"
}
```

Returned when the key is claimed but no response has been stored yet. The request is never re-executed. Under normal operation a concurrent duplicate waits for the winner's transaction and receives the replayed result instead.

### Validation failure — `400 Bad Request`

```json
{
  "error": "amount must be greater than zero"
}
```

Other messages: `invalid request body`, `idempotencyKey must contain 8 to 64 characters`, `fromWalletId must be greater than zero`, `toWalletId must be greater than zero`, `sender and receiver wallets must be different`.

Invalid requests are rejected **before any transaction starts**, so no idempotency key is reserved.

### Uncertain commit — `503 Service Unavailable`

```json
{
  "error": "transfer outcome uncertain; retry using the same idempotency key"
}
```

If the database outcome is uncertain (for example, the connection drops during COMMIT), the service first looks up the saved response by key and returns it if the transfer did commit. Only when the outcome cannot be confirmed does it return `503`; **never claim that the transfer failed**. The client must retry with the **same key**. A `503` response is not cached as a terminal business result.

### Unexpected error — `500 Internal Server Error`

```json
{
  "error": "internal server error"
}
```

Used for unclassified errors, including a retryable database error (deadlock, serialization failure) that persisted after all retry attempts. The transaction is rolled back, nothing is committed, and the client may retry with the same key.

## 4. Response matrix

| Condition | HTTP status | Stored transfer state | Retry guidance |
|---|---:|---|---|
| First successful transfer | `201` | `PROCESSED` | Same key returns original `201` response |
| First insufficient-funds transfer | `409` | `FAILED` | Same key returns original `409` response |
| Exact duplicate of committed request | Original status | Original state | No additional side effects |
| Same key, different payload | `409` | Unchanged | Use a new key for a new transfer |
| Same key, original still in progress | `409` | Unchanged | Retry shortly with the same key |
| Invalid request | `400` | None | Correct request; no key reserved |
| Wallet not found | `404` | None (rolled back) | Same key may be reused |
| Deadlock / serialization error | Retried internally (3 attempts, 100 ms then 200 ms backoff); `500` if exhausted | No committed outcome | Retry same key |
| Connection lost during COMMIT | Saved result if it committed, else `503` | Must be re-read to establish | Retry same key; do not use a new key |

## 5. Idempotency and concurrent requests

- `idempotency_records.idempotency_key` has a unique constraint (a composite unique key would be needed if scoped by authenticated principal).
- The request fingerprint is stored with the key.
- The transaction claims the key first (`INSERT … ON CONFLICT DO NOTHING`). The reservation, transfer row, wallet updates, two ledger entries, terminal transfer status, and replayable response are committed **atomically**.
- Concurrent requests for the same key are serialized through PostgreSQL's unique-key conflict handling. The loser waits for the winner's transaction to resolve, then reads and compares the committed fingerprint/result. If the winner rolls back, a retry may become the winner.
- The whole transaction is retried (up to 3 attempts) on retryable SQLSTATEs `40P01` (deadlock) and `40001` (serialization failure), not just a failed statement.
- Wallet rows are locked with `SELECT … FOR UPDATE` in **ascending wallet ID order**, and **before the transfer row is inserted**. Inserting the transfer first would take `FOR KEY SHARE` locks on both wallets through the foreign keys, and two concurrent transfers would then deadlock when upgrading to `FOR UPDATE`. Balances are checked under the lock.
- Keep transaction work limited to database operations; no outbound calls inside the transaction.

## 6. State contract

Allowed states: `PENDING`, `PROCESSED`, `FAILED`.

```text
PENDING -> PROCESSED
PENDING -> FAILED
```

`PROCESSED` and `FAILED` are terminal. In the chosen **single-database-transaction** design, `PENDING` is an intermediate uncommitted state. Normally, only the terminal state becomes externally visible after commit. This is intentional and does not require an asynchronous status endpoint.

- A `PROCESSED` transfer has exactly one debit and one credit ledger entry for the same positive amount.
- A `FAILED` transfer has no balance changes and no ledger entries.
- A crash before commit leaves no new committed transfer or ledger entries; a crash after commit leaves the full committed result.
- State transitions are guarded at the persistence layer (`UPDATE … WHERE id = ? AND status = 'PENDING'`, affected rows checked) as well as in the domain model.

## 7. Consistency invariants

For every committed successful transfer:

1. `source_balance_after = source_balance_before - amount`.
2. `destination_balance_after = destination_balance_before + amount`.
3. Neither wallet becomes negative (`CHECK (balance >= 0)`).
4. There are exactly two ledger entries: one `DEBIT` and one `CREDIT`.
5. The signed sum of the two ledger entries is zero.
6. The transfer is `PROCESSED` and the idempotency result is replayable.

For every committed business failure, wallet balances and ledger entries are unchanged. Database constraints and transaction logic enforce these invariants under concurrency.

## 8. Other endpoints

### GET /health

Liveness check, implemented.

```json
{
  "status": "ok",
  "service": "wallet-transfer-service"
}
```

### Optional endpoints (not implemented)

`GET /wallets/{walletId}/balance` and `GET /transfers/{transferId}` are optional in the assignment and are not implemented. Sketches only:

```json
{ "walletId": 1, "balance": 900 }
```

```json
{ "transferId": 1001, "status": "PROCESSED", "amount": 100 }
```

## 9. Observability

- Implemented: structured JSON logs through `slog` for startup, shutdown and server errors; GORM logs warnings and errors only by default (`DB_LOG_LEVEL=info` prints every SQL statement with its parameters, for local debugging only); `GET /health`.
- Not implemented: request/correlation IDs, per-transfer outcome logging, metrics, and a readiness endpoint that checks database connectivity.
- Never log credentials or unnecessary personal information; avoid logging full request bodies.
- Suggested metrics for later: transfer outcomes, request latency, transaction retries, deadlocks, and lock-wait duration.

## 10. Contract tests

Covered by the automated tests (`make test`, `make test-integration`; see `docs/TESTING.md`):

1. First valid transfer succeeds and creates exactly two ledger entries.
2. Duplicate same-key/same-payload request returns the original status/body without extra effects.
3. Same-key/different-payload request returns `409`.
4. Insufficient funds results in committed `FAILED`, unchanged balances, and zero ledger entries.
5. Same-key retry of a committed `FAILED` transfer returns the same failure even if the wallet is later funded.
6. Two concurrent debits cannot overdraw the same wallet.
7. Two simultaneous requests with the same idempotency key cannot create two transfers.
8. Opposite-direction transfers preserve balances and avoid deadlock.
9. Injected failure before commit rolls back all transfer effects.
10. Retry after simulated lost response (uncertain commit) returns the committed result.
11. Invalid input causes no committed side effects.

## 11. Decisions and remaining assumptions

- **Currency and amount unit:** The assignment uses integer `amount` but does not specify denomination. Define it explicitly before real-money use.
- **Authentication / key scope:** The assignment does not define users or tenants. A production API should scope keys to the authenticated caller.
- **Key retention:** No expiry is prescribed. Committed idempotency records are retained indefinitely; define a retention policy before production.
- **Wallet creation and funding:** The assignment does not define these APIs. Tests and demos use seeded wallets and balances.
- **Nonexistent wallet:** Decided: `404`, rolled back, key not reserved.
- **Business failure status:** Decided: `409` with `{"error":"insufficient funds"}`.
- **Exhausted transient retries:** Decided: `500` (the retried database error is not exposed). `503` is used only for an unconfirmed commit.
- **Idempotency key length:** Decided: 8–64 characters, validated in the handler and domain and enforced by the database.
- **Difference from the assignment's example:** the assignment's sample request uses string wallet ids (`"wallet_1"`) and a 6-character key (`"abc123"`). It is an example, not a format requirement; this service uses integer wallet ids and keys of 8–64 characters (see §2). `{"idempotencyKey":"abc12345","fromWalletId":1,"toWalletId":2,"amount":100}` is the equivalent request.

## 12. Implementation alignment

- **Fiber v3 handler** (`internal/feature/wallet/handler`): parse/validate JSON and map domain outcomes to HTTP.
- **Service** (`.../service`): orchestrate idempotency, retries and the transfer workflow.
- **Domain** (`.../domain`): validate invariants and allowed state transitions.
- **Repository** (`.../repository`): execute atomic operations in PostgreSQL, including unique constraints and row locks.
- **Route** (`.../route`): register `POST /transfers`.

The implementation is verified against this contract by the unit and integration tests.

# Client Retry Contract

This document defines what callers of `POST /transfers` should do when they
receive a non-2xx response. The contract is small and stable — implement it
once in your client and you'll preserve the exactly-once guarantee the API
offers.

## TL;DR

- **Always** send a fresh, durable `Idempotency-Key` per logical transfer
  attempt.
- **Always** retry the *same* idempotency key — never generate a new one for
  the same logical transfer.
- **Honor `Retry-After`** when the server returns 503.
- **Never** retry on 4xx — those are caller bugs, not transient failures.

---

## Idempotency Key Lifecycle

For each *logical* transfer (e.g. "user X sends 100 cents to user Y at
12:00:01"):

1. Generate a single `Idempotency-Key` (UUID v4 recommended, ULID accepted).
2. Persist that key locally **before** the first network call so a client
   crash does not lose it.
3. Use the same key for every retry of that logical transfer.
4. Stop retrying once you've received any 2xx, any 4xx, or exhausted the
   retry budget below.

The server scopes idempotency by `(clientId, idempotencyKey)`. The same key
under a different `X-Client-Id` header is treated as a different transfer.

Idempotency keys may be up to 256 characters. Choose values that do not carry
end-user PII — the key surfaces in error responses and logs (masked, but
prefix/suffix is preserved for correlation).

---

## Status Code Semantics

| Status | Meaning                                                                         | Retry-safe? |
|--------|---------------------------------------------------------------------------------|-------------|
| 201    | Transfer created                                                                | No (already done) |
| 200    | Replay of a prior 201 — same key, same payload                                  | No |
| 200    | Replay of a prior 422 — same key, same payload, original failure cached         | No |
| 400    | Validation error (bad request body, missing/oversized headers)                  | No — fix the request |
| 403    | Unknown `X-Client-Id`                                                           | No — fix client config |
| 404    | Wallet not found                                                                | No — confirm wallet IDs |
| 409    | Same idempotency key reused with a *different* payload                          | No — programmer error |
| 409    | Idempotency key currently in-flight on a peer request                           | Maybe — see [§ 409 In-Progress](#409-in-progress) |
| 422    | Insufficient balance                                                            | No — domain failure |
| 503    | Transient retry-budget exhaustion                                               | **Yes — honor `Retry-After`** |
| 504    | Database statement / lock / connection timeout                                  | Yes — same idempotency key |
| 5xx    | Other server error                                                              | Yes (cautiously) — same idempotency key |

### 409 In-Progress

Returned when a peer request with the same `(clientId, key)` is mid-execution
on the server. Two scenarios:

- **Your client did not send a peer request.** A network retry from your own
  stack is the most likely cause. Wait briefly (~1 s) and re-issue the same
  key — the server will return the cached result once the peer commits.
- **Your client did send a peer request.** Two threads in your client are
  racing on the same idempotency key. This is a client bug, not a server
  signal — fix the client.

### 503 Transient Conflict

Returned with `Retry-After: 1`. The server has already exhausted its internal
retry budget (3 attempts with jittered backoff) on a conflict-then-rollback
race that did not resolve. Sleep at least the indicated number of seconds,
then retry with the *same* idempotency key.

If the same `(client, key)` pair sees three consecutive 503s, escalate —
sustained 503s indicate a degraded server, not a momentary hot row.

The 503 body carries a `reason` field distinguishing
`RETRY_BUDGET_EXHAUSTED` (true contention) from `REQUEST_INTERRUPTED` (the
request thread was interrupted, typically during graceful shutdown of the
server). Both are retry-safe; the field exists for incident-response
clarity, not for divergent client behavior.

### 504 Gateway Timeout

The transaction timed out at the database (`statement_timeout=10s` or
`lock_timeout=8s` fired, or the connection itself failed). Server-side state
is rolled back: the idempotency record was either claimed and rolled back or
never claimed at all. Either way, retry with the same key is safe — the
server's `INSERT … ON CONFLICT` claim handles the race.

### Other 5xx

A server-side failure of an unexpected kind. Retry with the same idempotency
key is safe by the same INSERT/ON-CONFLICT guarantee, but back off
aggressively — a tight 5xx loop will turn a one-instance fault into a
self-DoS.

---

## Recommended Backoff

For 503 / 504 / 5xx:

```
attempt 1: 1 s + 0–1 s jitter
attempt 2: 2 s + 0–1 s jitter
attempt 3: 4 s + 0–1 s jitter
abort after 3 attempts
```

For 409 in-progress (when caused by a network retry from your own stack):

```
attempt 1: 1 s
attempt 2: 2 s
abort after 2 attempts
```

Jitter is required, not optional — without it, a fleet of clients retrying
on a fixed cadence will arrive at the server in lockstep and amplify the
hot-row contention they are trying to escape.

---

## Things Clients Must NOT Do

- **Do not retry 4xx.** A 4xx means the request was rejected on its merits.
  Retrying produces the same 4xx forever; the only outcome of a tight 4xx
  retry loop is a noisy log dashboard.
- **Do not generate a new idempotency key on retry.** The whole point of the
  key is to deduplicate retries. A new key on retry will be treated as a
  fresh logical transfer and may double-debit.
- **Do not retry without backoff.** A bare `while (response != 2xx) retry()`
  loop will turn a transient hot-shard event into a self-DoS.
- **Do not log full idempotency keys at INFO+.** They identify a specific
  transfer attempt and may carry caller-controlled content. Log a masked
  prefix/suffix (the server does — see `LogSafe.mask`).
- **Do not embed end-user PII in the idempotency key.** Keys appear in error
  responses and (masked) in server logs.

---

## Server Headers Echoed Back

The server echoes:

- `X-Correlation-Id` — taken from the request if you send one, otherwise
  generated by the server. Persist the same value across all retries of the
  same logical transfer for end-to-end log correlation. The server's MDC
  pattern includes this on every log line for the request.
- `X-Request-Id` — generated per HTTP request. Different on each retry
  attempt, even when the idempotency key is the same.

For 503 responses, the server also sets `Retry-After: 1` (seconds). Honor it.

---

## What Counts as "Same Payload" for Replay

The server SHA-256 hashes the canonical payload `(fromWalletId, toWalletId,
amount)` and stores the digest on the first claim. Replays must send byte-
identical values for those three fields. The hash is computed from the
parsed DTO, not the raw JSON, so:

- Whitespace and field ordering do **not** matter.
- Value differences **do** matter — including capitalization of wallet IDs,
  leading zeros on amounts, and unicode normalization. Any of these will
  surface as a 409 hash mismatch.

---

## Server-Side Retry Behaviour (For Reference)

When the server itself sees a transient conflict (a peer transaction claimed
the key, started writing, then rolled back), it retries internally with
jittered backoff between attempts. Only after exhausting that budget does it
surface 503 to the client. A 503 from this service is genuinely "I have
already tried hard and failed" — clients should not interpret it as "try
again immediately and the server will probably succeed".

The retry behaviour is configurable via `application.yml` (default values
shown):

| Property                              | Default | Effect                                      |
|---------------------------------------|---------|---------------------------------------------|
| `transfer.retry.max-attempts`         | 3       | Cap on internal attempts before 503         |
| `transfer.retry.base-backoff-millis`  | 10      | Base sleep between attempts                 |
| `transfer.retry.jitter-millis`        | 10      | Uniform jitter on top of base sleep         |
| `transfer.retry.retry-after-seconds`  | 1       | Value of the `Retry-After` header on 503    |

The corresponding environment-variable overrides (`TRANSFER_RETRY_*`) let
oncall tune them during an incident without a redeploy. If you observe
`transfers.execute.duration{outcome="transient_conflict"}` rising sharply,
look at the peer-transaction error rate before increasing `max-attempts` —
more retries on a genuinely failing dependency just amplifies tail latency.

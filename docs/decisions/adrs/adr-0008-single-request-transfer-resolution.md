---
kind: adr
status: accepted
owner: Kartik Mittal
last_reviewed: 2026-08-24
---

# ADR-0008 — Single-request transfer resolution: no background worker or queue

- **Status:** Accepted
- **Deciders:** Kartik Mittal (candidate)

## Context

`ASSIGNMENT.md` defines a transfer state machine (`PENDING -> PROCESSED`, `PENDING -> FAILED`)
but never mentions a background worker, queue, webhook, or polling mechanism. ADR-0002 and
ADR-0003 already assume a transfer resolves to a terminal state within one database transaction,
itself within one HTTP request — found on review to be an assumption never stated as its own
decision, despite being consequential enough to rule out an entire class of architecture.

## Decision drivers

- Nothing in `ASSIGNMENT.md` suggests deferred/asynchronous processing.
- A queue/worker architecture is materially larger scope than a 3-5 hour assignment affords.
- "State transitions must be safe under retries and duplicates" is trivially satisfiable if a
  transfer never sits in an intermediate state visible outside its own transaction — there's
  nothing for a retry to observe or collide with mid-flight.

## Considered options

1. **Asynchronous processing.** `POST /transfers` immediately returns `PENDING`; a background
   worker later resolves it; the client polls or is notified. Rejected: not hinted at anywhere in
   the spec, and the added infrastructure (worker, queue, polling/webhook contract) is
   disproportionate to both the time-box and the stated grading criteria, none of which mention
   asynchronous delivery.
2. **Synchronous single-request resolution.** `PENDING` exists only as in-transaction bookkeeping;
   the transfer is inserted and resolved to `PROCESSED` or `FAILED` within the same transaction,
   and the response always reflects the terminal outcome. *Chosen.*

## Decision

A transfer never durably exists in `PENDING` outside the transaction that resolves it.
`POST /transfers`'s response always carries the transfer's terminal state. No worker, queue, or
polling endpoint exists. Consequence for any future read API (optional transfer history): a
transfer will only ever be *observed* as `PROCESSED` or `FAILED` — nothing else can see it before
it resolves, so `PENDING` never appears in a read.

**Response status code (closes a gap found on review — no ADR previously specified this):**
`POST /transfers` returns `200 OK` for *both* terminal outcomes, `PROCESSED` and `FAILED` alike —
the response body's `status` field carries which one, plus a `failureReason` (e.g.
`"INSUFFICIENT_FUNDS"`) when `FAILED`. A `FAILED` transfer is not a service failure: the request
was well-formed, addressed two wallets that both exist, and was fully and correctly processed — it
resolved to a legitimate business outcome the caller needs to handle, not an error the transport
layer needs to signal. Reserving 4xx/5xx for what they're actually for keeps the status-code
contract meaningful:

- `200` — `PROCESSED` or `FAILED` (see body for which, and `failureReason` if `FAILED`).
- `404` — a referenced wallet doesn't exist (ADR-0003); nothing persisted.
- `409` — the idempotency key was reused for a different logical request (ADR-0002).
- `503` — bounded retry exhausted under lock contention (ADR-0003); nothing persisted, safe to
  retry with the same key.
- `4xx` (exact code decided in ADR-0005's OpenAPI spec) — pure request-shape validation failure
  (ADR-0002's validation boundary); nothing persisted.

This also keeps ADR-0002's idempotent replay simple: replaying a key whose transfer resolved to
`FAILED` returns the same `200` with the same body — replay never produces a different status
code than the original request did.

## Consequences

**Good:** trivially satisfies "safe under retries and duplicates," since nothing is ever left in
an inconsistent, resumable-by-someone-else state. Smaller surface area, matches the time-box.

**Bad / risks:** if the grader's mental model expected `PENDING` to be a genuinely observable,
longer-lived status (e.g. a "pending approval" style flow), this reading would need revisiting —
flagged explicitly as an interpretation, not a certainty, since `ASSIGNMENT.md` is silent on it
either way.

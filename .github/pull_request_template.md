## Summary

Describe your solution briefly.

## AI disclosure

Detail how you used AI to help with your submission (including the tools you used, how
you used them and what your prompts were).
Include these points in detail

1. What tool you used (Cursor, Claude Code, Antigratvity etc.)
2. How you generally use the tool for your work.
3. A transcript of your entire session with your AI tool of choice. You can add this to the repo or email it to us with your submission. If for some reason, this is not possible, give us all the prompts that you used with the AI.

## Schema Design

Describe the tables, constraints, and indexes you introduced.

## Idempotency Strategy

Explain how duplicate requests are handled safely.

## Concurrency Strategy

Explain how you prevent race conditions and double spending.

## How to Run

- 

## How to Test

- 

## Tradeoffs / Assumptions

- 

## Implementation notes (for reviewers)

- Idempotency: pre-claiming semantics implemented (`PENDING` → `COMPLETED`/`FAILED`), server-side `requestHash`, response replay, and a cleanup worker with metric `wallet_transfer_idempotency_pending`.
- Transfer IDs: server generates `transferId` when omitted by client; clients may supply an ID but it's optional.
- Concurrency: pessimistic locking (`SELECT ... FOR UPDATE`) is used; a `version` column exists for future optimistic-locking work.
- Reconciliation: stored balance updates are performed in the same transaction as ledger writes; a reconcile endpoint exists for verification.

## Checklist

- [ ] Tests pass
- [ ] Lint passes
- [ ] Format check passes
- [ ] README or notes updated
- [ ] PR description explains schema, idempotency, and concurrency
 - [ ] PR description explains schema, idempotency, concurrency, and any implementation deviations from ADRs

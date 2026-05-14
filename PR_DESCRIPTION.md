# Wallet Transfer Service — Implementation

## Overview

Implemented a wallet-to-wallet transfer service in Go with PostgreSQL, covering idempotency, double-entry ledger, and safe concurrent execution.

---

## Database schema

Three core tables:

**`wallets`** — stores the running balance per wallet.  
`balance BIGINT CHECK (balance >= 0)` provides a hard DB-level floor; the application additionally validates funds before issuing the UPDATE.

**`transfers`** — one record per logical transfer request.  
`UNIQUE(idempotency_key)` is the primary deduplication mechanism.  
`status CHECK IN ('PENDING', 'PROCESSED', 'FAILED')` enforces the state machine at the DB level.

**`ledger_entries`** — double-entry records; two rows per transfer (DEBIT + CREDIT).  
`REFERENCES transfers(id)` ensures entries can never be orphaned.

Key indexes: `idempotency_key` (unique), `from_wallet_id`, `to_wallet_id`, `wallet_id` on ledger.

---

## Idempotency strategy

The service issues:

```sql
INSERT INTO transfers (…) ON CONFLICT (idempotency_key) DO NOTHING RETURNING *
```

- If the INSERT returns a row → new transfer, proceed to process it.
- If it returns nothing (conflict) → fetch the existing record and return it immediately, no side effects replayed.

This is enforced at the database layer, not in application memory, so it is safe across process restarts and concurrent duplicate requests.

**Known limitation:** A crash after the PENDING record is committed but before the transaction completes leaves the transfer in PENDING. A retry with the same key returns PENDING rather than re-driving it. In production this would be handled by a reconciliation job.

---

## Concurrency handling

Row-level locking (`SELECT … FOR UPDATE`) inside a single database transaction:

1. Both wallet rows are locked before any balance read or write.
2. Locks are acquired in **sorted wallet ID order** to prevent two transactions from deadlocking while each waits for the other's lock.
3. The balance check happens while the lock is held — no concurrent debit can race between the read and the write.
4. Balance changes, ledger entries, and the `PROCESSED` status transition are all committed atomically, so the status is never visible without the corresponding balance movement.

---

## Assumptions and tradeoffs

- **Amounts are integers** (smallest currency unit, e.g. cents). No floating-point arithmetic anywhere in the transfer path.
- **Pessimistic locking over optimistic** — shorter, predictable critical section vs. unbounded retries under contention.
- **No migration framework** — a single `001_init.sql` file applied once. A production system would use `golang-migrate` or similar.
- **In-process idempotency** — no separate `idempotency_records` table; the `UNIQUE` constraint on `transfers.idempotency_key` is sufficient.
- **FAILED is terminal** — retrying a failed transfer requires a new idempotency key, consistent with typical payment API conventions (Stripe, etc.).

---

## AI usage

**Tool:** Claude (claude.ai)

**How I use it:** I use Claude as a technical sounding board and first-draft generator. My workflow is:
1. Sketch the design and key decisions myself first.
2. Ask Claude to write the boilerplate and first-pass implementation.
3. Review every file carefully — checking correctness of SQL, transaction boundaries, locking strategy, and error handling.
4. Iterate on specific sections (e.g. the idempotency flow and deadlock prevention) with targeted follow-up prompts.
5. Own all final decisions; Claude doesn't replace engineering judgment.

I don't blindly accept generated code — I pushed back on several initial choices (e.g. an early version updated the PROCESSED status outside the transaction, which would have created an inconsistency window; I caught this and asked for a corrected version with the status update inside the same transaction).

A transcript of the session has been included in `ai-session-transcript.md` in this repository.

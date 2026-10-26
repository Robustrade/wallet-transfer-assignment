# Wallet transfer design and invariants

## Scope

A synchronous, single-currency wallet-to-wallet transfer service. A submitted operation has a required idempotency key, two distinct pre-existing wallet IDs, and a positive signed 64-bit amount in minor currency units. Only `POST /transfers` is required and exposed.

## Database schema

**`wallets`**: `id TEXT PRIMARY KEY`, `opening_balance BIGINT NOT NULL CHECK >= 0`, `balance BIGINT NOT NULL CHECK >= 0`, `created_at`. The opening balance is immutable after insertion via trigger; controlled seed data starts current and opening values equal. Operationally, balances change only through the transfer service.

**`transfers`**: UUID PK, globally unique nonnull `idempotency_key`, wallet FKs, positive BIGINT amount, `PENDING|PROCESSED|FAILED` status, nullable failure reason with a consistency check, creation timestamp. A check prohibits self-transfers; wallet FK constraints protect references. The unique key index implements durable request arbitration.

**`ledger_entries`**: identity PK, transfer FK, wallet FK, `DEBIT|CREDIT`, positive BIGINT amount, creation timestamp, `UNIQUE(transfer_id, entry_type)` and an index by wallet. An append-only trigger forbids modifications/deletions to posted entries.

Some checks in the database are **necessary, but not sufficient** to enforce the complete business invariant: SQL checks here do not themselves require exactly two rows for every processed transfer, matching wallet IDs/amounts, or consistency between the wallet's cached balance and ledger postings. Those are established by the one controlled transactional write path and verified with integration tests. A privileged actor can still update stored wallet balances or a terminal transfer row outside the service; access control and operational reconciliation would be needed for production use.

## Transaction algorithm

1. Parse HTTP and validate required fields, IDs, amount, and distinct source/destination. Structural rejections are not durably recorded.
2. `BEGIN` at PostgreSQL `READ COMMITTED`. Insert a `PENDING` transfer using `ON CONFLICT (idempotency_key) DO NOTHING RETURNING id` as the **first claim**. The unique index coordinates same-key concurrent requests.
3. If the insert conflicts, fetch the existing committed row in a **new SQL statement** (important with `READ COMMITTED` snapshots). Compare the original source, destination and amount. Identical request: return immutable saved terminal outcome. Different request: idempotency conflict. Do not access or update wallets on replay. A conflicting key takes precedence over wallet existence even when the submitted destination doesn't exist.
4. If newly claimed, acquire both wallet rows in ascending wallet-ID order, using one `SELECT ... FOR NO KEY UPDATE` per wallet. Wallet existence is also protected by FKs on the newly inserted transfer. This prevents read/modify races and the classic opposite-direction AB/BA lock inversion. `FOR NO KEY UPDATE` still blocks competing balance writers while remaining compatible with normal key-share FK checks.
5. If source balance is too small, update `PENDING -> FAILED` with reason `INSUFFICIENT_FUNDS` and commit **without financial postings**.
6. Otherwise, debit conditionally (`WHERE balance >= amount`), credit receiver, insert exactly one matching debit and credit ledger entry, then guardedly transition to `PROCESSED`. All statements run in the **same transaction**. Every unexpected error, overflow or row-count mismatch aborts the transaction. The return value is used only after a confirmed commit.
7. A known PostgreSQL deadlock (`40P01`) or serialization abort (`40001`) retries the **whole** transaction, bounded to three attempts with short backoff. Other errors, especially an uncertain connection/commit outcome, are not automatically retried by the service. Clients retry using the **same** idempotency key once they can reconnect.

## Accounting invariant

For each wallet after a committed operation:

`stored_balance = opening_balance + SUM(ledger CREDITS) - SUM(ledger DEBITS)`.

Successful transfers have two equal postings, from the source and to the destination, and conserve money across wallets. Insufficient-funds attempts create a terminal `FAILED` transfer row and **no ledger entries**. This is a deliberate interpretation of the assignment's ambiguous statement that “every transfer” has two entries: unsuccessful attempts don't move funds and therefore don't have accounting postings.

There is no funding API. An immutable **opening balance** describes externally provisioned funds without fabricating a deposit event. For testing a failed transfer after funds arrive, a reserve wallet sends a normal recorded transfer; arbitrary updates to `wallets.balance` would break reconciliation.

## Idempotency and state semantics

`transfers.idempotency_key` serves as the durable idempotency record because the service has one operation type and a deterministic response (`transferId`, status, optional failure reason). No extra idempotency table or serialized response cache is necessary. Replays use the original status code and JSON body. An insufficient-funds failure remains terminal even after funds arrive; a new attempt needs a new key. The key is retained indefinitely for this exercise.

`PENDING` is an **in-transaction** intermediate state. Only `PROCESSED` or `FAILED` is committed; crashes before commit roll back the key and all effects. There is no externally observable or recoverable asynchronous pending workflow. This meets the synchronous-state-transition interpretation, but not a hypothetical requirement for durable pending jobs.

Early rejected structural requests and new-key missing-wallet errors are not stored and therefore are outside durable HTTP-error replay. Retries must keep the same key and payload. The API does not promise one-time delivery of a network response—only that a logical accepted key does not cause duplicate committed financial effects.

## Remaining boundaries

- Ledger completeness and reconciliation are checked by the service path and tests, not by deferred cross-table constraint triggers.
- No direct client authentication, TLS, account funding API, account freeze/reservation, exchange rates, reversals or external financial effects.
- PostgreSQL BIGINT arithmetic errors abort the transaction; the added overflow regression was **not yet run against local PostgreSQL at final artifact creation**.
- Commit-time connection loss has not been fault-injected; known successful-commit/lost-response replay has been verified.
- Credit/claim/wallet locks can wait on unrelated long transactions; there is no additional explicit database lock timeout beyond request/server lifetimes.
- The migration assumes a fresh schema; schema versioning or zero-downtime upgrade management is out of scope.

## Why this design

It prioritizes atomic accounting, durable replay, predictable lock order, and comprehensible Go layering over unnecessary infrastructure. The full code path is small enough to explain during the interview, and PostgreSQL integration tests exercise real constraints and concurrency semantics.

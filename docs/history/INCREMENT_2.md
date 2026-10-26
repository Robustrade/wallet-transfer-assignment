> Historical implementation note. Verification status in this snapshot may be superseded; see the root README and docs/TEST_PLAN.md for the current status.

# Increment 2 — Service + PostgreSQL transaction design

## Implementation sequence and scope

**Red tests first:** `internal/service/transfer_integration_test.go` was authored before the repository and service. Because PostgreSQL and the driver cannot run in the authoring container, the red test failure was not executed there; running it on the reviewer's machine is required for verification.

**Minimal implementation second:** `internal/service/transfer.go`, `internal/repository/postgres.go`, and `internal/domain/outcome.go` implement only the happy path, insufficient funds, and the idempotency-key claim/replay needed to initiate either safely. Concurrency stress tests and HTTP transport are out of scope for this increment.

## Ownership boundaries

- **Domain:** request structure, status and failure constants, result/error types.
- **Service:** determines what to do when a key is new, locks rows in stable order, chooses a terminal business outcome, and orders debit/credit/ledger writes.
- **Repository:** executes SQL, controls the transaction boundary, claims keys, guards updates, reads stored results, and commits or rolls back.

## Transaction flow

1. Validate basic request fields before opening a DB transaction.
2. In `READ COMMITTED`, try to insert the `PENDING` transfer using unique key + `ON CONFLICT DO NOTHING RETURNING id`.
3. If the insert reports an existing key, fetch the committed transfer in a new statement, compare request details, and replay the stored terminal result or reject reuse with different details. Incoming wallet existence is **not** checked before this branch.
4. If claimed, acquire `FOR NO KEY UPDATE` on the lower wallet ID and then the higher ID in separate statements. Read the sender balance from the locked row.
5. Insufficient funds → update to `FAILED` with reason, commit. Balances and ledger remain unchanged.
6. Sufficient funds → guarded debit, credit, paired ledger insert, guarded `PENDING -> PROCESSED` update, commit.
7. Any failed statement before commit → rollback. An uncertain commit → return an error, do not report a terminal financial failure; same-key retry will resolve it when the database is reachable.

## Guarantees: DB vs application

| Guarantee | Enforced by |
|---|---|
| One committed transfer per idempotency key | PostgreSQL UNIQUE index; insert conflict resolution |
| Nonexistent wallet cannot be used for a newly created transfer | Foreign keys; `23503` translated to missing wallet |
| Wallet balances cannot go below zero | `CHECK(balance>=0)`, guarded debit, row locks |
| Balance updates/ledger/status commit as one unit | Single PostgreSQL transaction |
| Opposite-direction transfers request wallet locks in the same order | Service stable ID sort; not yet stress-tested |
| Successful transfer has the correct two entries | Business transaction logic + integration assertion; UNIQUE only enforces at most one per type |
| Failed transfer has no entries | Business logic + integration assertion |
| Current balances reconcile with opening balance and ledger | Business logic + integration assertion |
| Valid state transition `PENDING -> terminal` | Guarded SQL update; no general DB trigger for arbitrary writes |

## Deliberate limits

- Existing migration's opening-balance and ledger immutability triggers are unchanged.
- Seeded opening balances don't themselves record historical funding; test replenishment will use `reserve -> A` as a normal transfer when the replay test is added.
- Missing-wallet new keys fail at foreign-key validation without committing a transfer; the original key lookup takes precedence for existing keys.
- The immutable original result can be reconstructed from `id`, `status`, and `failure_reason` (API response contract will be fixed later).
- No retries are attempted automatically on deadlock, serialization, or uncertain-commit errors. A bounded retry policy can be added alongside concurrency tests.
- DB constraints do not prove cross-table amount equality, total money conservation or correct posting order independently of application code. Tests must.
- No claim of end-to-end verification until PostgreSQL tests run successfully on the user's machine.

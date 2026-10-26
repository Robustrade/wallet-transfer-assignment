# Final review against Robustrade's assignment and rubric

Reviewed the full Go HTTP, service and repository path, SQL migration, idempotency, transaction and concurrency tests, and documentation against `ASSIGNMENT.md`, `evaluation_guide.md`, and `.github/pull_request_template.md`.

## Corrections made in the final package

1. **Misclassified foreign-key errors:** Previously every PostgreSQL `23503` was returned as `wallet_not_found`, even an unexpected ledger/transfer reference error. Classification is now limited to the source/destination FK of a newly claimed transfer. Other FK errors remain internal failures. Added a focused unit test.
2. **Invalid UTF-8 identifiers:** Service-level request validation now explicitly rejects invalid UTF-8 before database access. Added domain unit cases. (HTTP JSON decoding already handles invalid byte sequences, so this mainly protects direct service callers.)
3. **Untested credit overflow:** Added an isolated PostgreSQL integration test deliberately overflowing the receiver's `BIGINT` after the sender's conditional debit has executed. It checks rollback of wallet updates, transfer claim and ledger, then frees credit capacity with an ordinary transfer and retries the same key. **This test requires local PostgreSQL verification.**

These are targeted hardening changes; no transaction architecture or previous migrations were replaced.

## Evaluation alignment

| Criterion | Evidence | Assessment |
|---|---|---|
| Schema | 3 tables, FKs, checks, uniqueness, useful ledger index, immutable openings/ledger | Meets focused assignment needs |
| Transaction safety | Explicit `READ COMMITTED` scope, sorted `FOR NO KEY UPDATE`, conditional debit, paired ledger and guarded terminal update | Strong, with tests |
| Idempotency | Unique key claim, fresh read after conflict, payload matching, terminal durable replay, unknown-outcome instructions | Strong, with tests |
| Concurrency | Postgres lock wait observation, opposing transfers, independent wallets, bounded `40P01`/`40001` retries | Strong for tested cases |
| Code structure | Thin HTTP handler, service orchestration, repository SQL, domain validation | Appropriate and minimal |
| Testing | Real DB, rollback injection, concurrency, HTTP, reconciliation and no-skip verification | Strong for confirmed suites; final additions pending |
| Development | README, design, tests, reproducible commands, AI disclosure and PR draft | Ready after final verification/transcript |

## Residual risks and decisions not to overengineer

- The database alone cannot demand two exact ledger entries for every `PROCESSED` row or automatically reconcile a cached balance. The service transaction writes these atomically; tests audit the outcome. Adding cross-table triggers now would complicate a focused assignment without evidence of an operational defect.
- `PENDING` is never durably committed, and terminal rows are protected from modification by service logic rather than an additional DB terminal-state trigger. SQL users with broad write privileges can bypass the service's invariants. Restricting writers and operational reconciliation would be necessary in production.
- The pre-seeded opening balance isn't backed by an external funding ledger. This is intentional because creating/funding wallets is not requested.
- A receiver credit overflow yields a database error rather than a specific domain-level business rejection, but PostgreSQL must abort the transaction; the new test checks this. Invalid amounts that fit int64 but overflow a receiver are rare, and new HTTP error taxonomy is not required.
- DB connectivity failures at unknown COMMIT outcome are reported as unknown; the client is directed to retry the same key. Exact commit-time disconnect injection has not been tested.
- `23503` classification assumes default PostgreSQL constraint names in the provided SQL migration. Renaming FK constraints would require updating this narrow mapping.
- No HTTP auth, multi-currency, GET endpoints, asynchronous tasks, distributed locks, or additional storage was introduced: none is required by the assignment.

**Recommendation:** Submit this architecture once the final added tests pass on PostgreSQL. Do not claim enterprise financial compliance or more failure coverage than the tests demonstrate.

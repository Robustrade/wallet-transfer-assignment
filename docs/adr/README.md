# Architecture Decision Records

Each file records one decision: its context, the decision, and its
consequences. Alternatives and full details are in the
[design doc](../design.md).

| ADR | Decision | Status |
|-----|----------|--------|
| [001](001-use-postgresql.md) | Use PostgreSQL, with embedded Postgres in tests | Accepted |
| [002](002-store-wallet-balances.md) | Store balances on the wallet row | Accepted |
| [003](003-record-opening-balances-on-wallets.md) | Record opening balances on the wallet | Accepted |
| [004](004-store-idempotency-key-on-transfers.md) | Store the idempotency key on the transfer | Accepted |
| [005](005-run-each-transfer-in-one-transaction.md) | Run each transfer in one database transaction | Accepted |
| [006](006-lock-wallets-in-id-order.md) | Lock both wallets in ID order with `FOR NO KEY UPDATE` | Accepted |
| [007](007-enforce-cross-row-invariants-in-code.md) | Enforce cross-row invariants in code, not triggers | Accepted |
| [008](008-test-through-the-api-without-mocks.md) | Test through the API, without mocks | Accepted |

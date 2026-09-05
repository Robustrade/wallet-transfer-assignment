# Transaction Flow

`POST /api/v1/transfers` uses one PostgreSQL transaction. It inserts or locks the idempotency record, compares the SHA-256 request hash, locks both wallet rows with `FOR UPDATE` in sorted ID order, creates a pending transfer, validates funds, updates both balances, inserts one debit and one credit ledger entry, records the final response, and commits.

A duplicate request with the same payload returns the original transfer without side effects. A different payload using the same key returns `409 Conflict`. Insufficient funds are recorded as `FAILED` without balance or ledger changes. Unexpected database errors roll back the transaction.

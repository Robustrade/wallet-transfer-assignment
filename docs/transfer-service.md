# Transfer service

The transfer service coordinates wallet, transfer, and ledger repositories through one PostgreSQL transaction.

## Wiring

Create the service with a transaction manager backed by the configured database pool:

```go
manager := service.SQLTransactionManager{DB: db}
transferService := service.NewTransferService(manager)
```

The transaction manager is deliberately separate from the repository implementations. Inside each execution, the service constructs all three repositories with the same transaction, ensuring every write shares one commit/rollback boundary.

## Execution behavior

1. Validate wallet IDs, distinct source/destination, and positive amount.
2. Begin a database transaction.
3. Lock both wallets with `SELECT ... FOR UPDATE` in lexicographically sorted wallet-ID order.
4. Validate both wallet records.
5. Insert a `PENDING` transfer.
6. Check currency compatibility, source funds, and destination balance overflow.
7. For a deterministic business failure, transition the transfer to `FAILED` and commit that result without balance or ledger changes.
8. Otherwise debit the source, credit the destination, insert one DEBIT and one CREDIT ledger entry, and transition the transfer to `PROCESSED`.
9. Commit before returning a successful result.

Unexpected repository, ID-generation, or commit errors return an error and leave the deferred rollback to undo the transaction.

## Current boundaries

This service does not implement idempotency-key handling or HTTP status/body mapping. Those are separate subsequent phases. Until idempotency is added, callers must not treat a retry as the same logical operation.

A business-rule failure returns the persisted FAILED transfer together with its domain error. The future HTTP handler will map those errors to the agreed API response codes.

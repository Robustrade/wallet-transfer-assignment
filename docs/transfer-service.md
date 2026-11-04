# Transfer service

The transfer service coordinates wallet, transfer, ledger, and idempotency repositories through a single PostgreSQL transaction.

## Wiring

Create the service with a transaction manager backed by the configured database pool:

```go
manager := service.SQLTransactionManager{DB: db}
transferService := service.NewTransferService(manager)
```

## Execution behavior

The idempotent entry point is `ExecuteIdempotent(ctx, idempotencyKey, request)`. It validates the key and request, hashes the canonical request fields, claims the key, and performs the transfer in the same transaction. The stored response is written before commit.

The transfer itself:

1. Locks both wallets with `SELECT ... FOR UPDATE` in lexicographically sorted wallet-ID order.
2. Validates both wallet records and inserts a `PENDING` transfer.
3. Checks currency compatibility, source funds, and destination balance overflow.
4. For a deterministic business failure, transitions the transfer to `FAILED` without changing balances or ledger entries.
5. Otherwise debits the source, credits the destination, inserts one DEBIT and one CREDIT ledger entry, and transitions the transfer to `PROCESSED`.

Unexpected repository, ID-generation, response-encoding, or commit errors roll back the complete transaction.

## Idempotency behavior

A matching key and request replays the original transfer result. Reusing a key with a different request returns `service.ErrIdempotencyConflict`. Successful results are stored with response status 201; deterministic business failures are stored with response status 422 and replay the same domain error. Infrastructure failures are not stored because their transaction rolls back.

The key claim uses PostgreSQL's unique constraint and `INSERT ... ON CONFLICT DO NOTHING`. Concurrent requests using the same key serialize on that unique key. See [idempotency.md](idempotency.md) for the request hash and concurrency details.

The service uses the same transaction for idempotency claim, wallet locks, transfer state, balance changes, ledger entries, and stored response. The HTTP layer will map these service results to the external API response format.

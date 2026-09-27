# Wallet Transfer Service - Design

## Understanding

The service will provide a POST /transfers API to transfer money between two wallets.

The main requirements are:

- Maintain correct wallet balances
- Handle duplicate requests using idempotencyKey
- Maintain a double-entry ledger
- Handle concurrent transfers safely
- Ensure transfers are atomic

## Approach

I will use PostgreSQL for persistence and keep the code separated into:

- Handler - API request/response handling
- Service - transfer business logic
- Repository - database operations
- Models - wallet, transfer and ledger entities

## Transaction Handling

A transfer will be performed inside a single database transaction.

The transaction will:

1. Check the idempotency key.
2. Lock the required wallet rows.
3. Check the sender's balance.
4. Update both wallet balances.
5. Create the transfer and ledger entries.
6. Store the idempotency result.
7. Commit the transaction.

If any step fails, the transaction will be rolled back.

## Concurrency

I will use PostgreSQL row-level locking FOR UPDATE to prevent concurrent transfers from incorrectly updating the same wallet balance.

Wallets will be locked in a consistent order to reduce the possibility of deadlocks.

## Idempotency

The idempotencyKey will be stored with a unique database constraint.

If the same key is received again, the existing transfer result will be returned and no new transfer entries will be created.


Each successful transfer will create exactly two entries:

- DEBIT for the source wallet
- CREDIT for the destination wallet

Both entries will belong to the same transfer.

## Testing

I will add tests for:

- Successful transfers
- Insufficient balance
- Invalid wallets
- Duplicate requests
- Ledger correctness
- Concurrent transfers
- Transaction rollback/failure cases

The focus will be on correctness, concurrency safety and retry-safe behavior.

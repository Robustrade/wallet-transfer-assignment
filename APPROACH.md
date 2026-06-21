# Initial Understanding and First Approach Ideas

## Problem:

We have to ensure wallet to wallet transfering of funds without repetition, handling and reporting failure and with balance correctness.

## Approach:

### The Point of concerns:

1. Preventing duplicate transfers when same idempotencyKey is retried.
2. Double ledger entries for each transaction: one debit and one credit
3. Keeping change in balance correct under concurrent transfer requests.
4. Handling transfer states safely using PENDING, PROCESSED and FAILED.
5. Multiple Transfers from single wallet should happen sequentially (no 2 debits at once).
6. Avoid concurrent attempt to debit from single wallet (one after another transaction flow).

## First thought of approach:

### A: Successful Transaction

1. Validate incoming API Request
2. Check an idempotency record using provided idempotencyKey.
3. Start a database transaction.
4. Create a transfer Record with Pending Status
5. Lock or safely update the wallet rows to prevent concurrent attempts of debit.
6. Validate the amount of debit availability in wallet.
7. Debit from source wallet and Credit to Destination wallet.
8. insert debit and credit ledger entries.
9. Mark transfer Record as Processed status.
10. Store final idempotency result.
11. Commit Transaction

### B: Failed Transaction

1. Validate field request before starting the transfer
2. If request is invalid, return a validation error.
3. Check an idempotency record using provided idempotencyKey.
4. Start a database transaction.
5. Create a transfer record with status PENDING.
6. Lock the wallet rows to prevent concurrent attempts of debit.
7. Validate wallet existence and source wallet balance inside the transaction.
8. If the transfer cannot be completed, mark the transfer as FAILED.
9. Do not debit/credit wallet balances.
10. Do not create successful debit/credit ledger entries.
11. Business Logic error: Commit Transaction for FAILED records (low balance, wallet not found....).
12. For Technical errors: rollback the transaction so partial wallet updates or ledger entries are not persisted.

## Database Design Direction:

I am considering PostgreSQL tables such as:

* wallets
* transfers
* ledger_entries
* idempotency_records

### A: wallets:

1. wallet_id as primary key
2. wallet.balance always non-negative.

### B: transfers

1. transfer_id as primary key
2. source_wallet_id and destination_wallet_id as foreign keys to wallets.
3. transfers.status can only have PENDING, PROCESSED, FAILED.
4. Indexes on: source_wallet_id and destination_wallet_id

### C: ledger_entries

1. entry_id as primary key
2. transfer_id as foreign key to transfers.
3. wallet_id as foreign key to wallets.
4. transaction_type can only have DEBIT and CREDIT.
5. Index on: transfer_id

### D: idempotency_records:

The idempotency_records table will help make the API retry-safe by storing the idempotencyKey, processing status

1. idempotency_record_id as primary key
2. idempotencyKey as unique entry
3. status can only have IN_PROGRESS, COMPLETED, and FAILED.
4. transfer_id as foreign key to transfers.
5. Index on: idempotencyKey

Common to all entities/table: createdAt/updatedAt as date-time

## Testing Strategies:

1. All correct data request for Successful order.
2. Invalid wallet scenario.
3. duplicate request with same idempotencyKey.
4. Ledger entry validation.
5. Insufficient balance case.
6. Concurrent transfer attempts from the same wallet.
7. Retry-safe behaviour where a request is repeated after simulated timeout or duplicate delivery.

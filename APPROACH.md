# Initial Understanding and First Approach Ideas

## Problem:

We need to ensure wallet-to-wallet transferring of funds without duplication, correctly handling/reporting failures, and maintaining correct balances.

## Approach:

### Points of concern:

1. Preventing duplicate transfers when same idempotencyKey is retried.
2. Double ledger entries for each transaction: one debit and one credit.
3. Keeping change in balance correct under concurrent transfer requests.
4. Handling transfer states safely using PENDING, PROCESSED and FAILED.
5. Multiple Transfers from single wallet should happen sequentially (no 2 debits at once).
6. Avoid concurrent attempt to debit from single wallet (one after another transaction flow).

## First thought of approach:

### A: Successful Transaction

1. Validate incoming API request.
2. Insert/claim an idempotency record using provided idempotencyKey with UNIQUE constraint and status IN_PROGRESS.
3. If idempotencyKey already exists, return the stored result/status for COMPLETED/FAILED or return still processing response for IN_PROGRESS.
4. Start the wallet transfer database transaction.
5. Create a transfer Record with Pending Status.
6. Lock or safely update the wallet rows to prevent concurrent attempts of debit.
7. Validate the amount of debit availability in wallet.
8. Debit from source wallet and Credit to Destination wallet.
9. Insert debit and credit ledger entries.
10. Mark transfer Record as Processed status.
11. Update the idempotency record to COMPLETED and store final idempotency result.
12. Commit Transaction.

### B: Failed Transaction

1. Validate field request before starting the transfer.
2. If request is invalid, return a validation error.
3. Insert/claim an idempotency record using provided idempotencyKey with UNIQUE constraint and status IN_PROGRESS.
4. If idempotencyKey already exists, return the stored result/status for COMPLETED/FAILED or return still processing response for IN_PROGRESS.
5. Start the wallet transfer database transaction.
6. Create a transfer record with status PENDING.
7. Lock the wallet rows to prevent concurrent attempts of debit.
8. Validate wallet existence and source wallet balance inside the transaction.
9. If the transfer cannot be completed, mark the transfer as FAILED.
10. Do not debit/credit wallet balances.
11. Do not create successful debit/credit ledger entries.
12. For expected business failures, commit the transaction for FAILED records after storing the failed idempotency result.
13. For unexpected technical errors, rollback the transaction so partial wallet updates or ledger entries are not persisted.

## Database Design Direction:

I am considering PostgreSQL tables such as:

* wallets
* transfers
* ledger_entries
* idempotency_records

### A: wallets:

1. wallet_id as primary key
2. wallet.balance always non-negative, enforced with a CHECK constraint such as balance >= 0.

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

The idempotency_records table will help make the API retry-safe by storing the idempotencyKey, processing status, transfer reference, and final result details needed to return the same result for duplicate retries.

1. idempotency_record_id as primary key
2. idempotencyKey with UNIQUE constraint
3. status can only have IN_PROGRESS, COMPLETED, and FAILED.
4. transfer_id as foreign key to transfers.
5. result_status/result_message to return previous result for duplicate retries.
6. Index on: idempotencyKey

Common to all entities/table: createdAt/updatedAt

## Testing Strategies:

1. All correct data request for Successful order.
2. Invalid wallet scenario.
3. duplicate request with same idempotencyKey.
4. Ledger entry validation.
5. Insufficient balance case.
6. Concurrent transfer attempts from the same wallet.
7. Retry-safe behaviour where a request is repeated after simulated timeout or duplicate delivery.

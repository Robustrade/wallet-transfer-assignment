# Wallet Transfer Service — Design

## 1. Problem

Build a wallet-to-wallet transfer service that guarantees:

* Idempotent request handling
* Atomic transfers
* Correct wallet balances
* Double-entry ledger entries
* Safe concurrent execution
* Retry-safe state transitions

## 2. API

### POST /transfers

Request:

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

A successful transfer returns the transfer identifier and its processed state.

The same request with the same `idempotencyKey` returns the original result without creating another transfer or ledger entries.

Reusing an `idempotencyKey` with different request parameters returns a conflict.

## 3. Database Design

The service uses PostgreSQL with four primary tables:

* `wallets` — stores the current wallet balance.
* `transfers` — records transfer requests and their lifecycle state.
* `ledger_entries` — records the double-entry accounting entries.
* `idempotency_records` — stores request identity and the original result for safe retries.

Important database constraints:

* Primary keys on all entities.
* Foreign keys between related entities.
* Unique constraint on `transfers.idempotency_key`.
* Unique constraint on `idempotency_records.idempotency_key`.
* Positive transfer and ledger amounts.
* Source and destination wallets must be different.
* Transfer status is restricted to `PENDING`, `PROCESSED`, or `FAILED`.
* A transfer can have at most one `DEBIT` and one `CREDIT` ledger entry.

## 4. Transaction Strategy

A transfer is executed inside a single PostgreSQL transaction.

The transaction:

1. Handles the idempotency record.
2. Locks the source and destination wallet rows.
3. Validates the source balance.
4. Updates both wallet balances.
5. Creates the transfer and its two ledger entries.
6. Moves the transfer to `PROCESSED`.
7. Stores the result required for idempotent replay.
8. Commits the transaction.

Any failure causes the transaction to roll back.

Wallet rows are locked using PostgreSQL row-level locking (`SELECT ... FOR UPDATE`).

When two wallets are involved, they are always locked in deterministic ID order to reduce deadlock risk.

## 5. Idempotency

Idempotency keys are persisted in PostgreSQL rather than kept in application memory.

A unique database constraint prevents two concurrent requests from creating separate transfers for the same key.

The request parameters are represented by a request hash.

For a retry:

* Same idempotency key + same request → return the original result.
* Same idempotency key + different request → return a conflict.
* A committed transfer remains replayable after a process restart.

This prevents duplicate financial side effects.

## 6. Transfer State Machine

Allowed states:

```text
PENDING → PROCESSED
PENDING → FAILED
```

A processed or failed transfer cannot be processed again.

The state transition occurs within the transaction so that transfer state and financial side effects remain consistent.

## 7. Ledger Model

Each successful transfer creates exactly two ledger entries:

```text
DEBIT  source wallet      amount
CREDIT destination wallet amount
```

Both entries reference the same transfer.

The ledger provides the audit trail for the movement of funds.

## 8. Balance Consistency

The current wallet balance is stored in the `wallets` table for efficient reads.

Balance updates happen in the same transaction as the corresponding ledger entries.

Concurrent transfers cannot read and update the same wallet balance independently because the wallet row is locked before the balance is checked and modified.

## 9. Error Handling

The API distinguishes between:

* Invalid request
* Wallet not found
* Insufficient funds
* Idempotency conflict
* Internal/database failure

Financial state is never partially committed when an operation fails.

## 10. Testing Strategy

Tests will cover:

* Successful transfer
* Insufficient balance
* Wallet validation
* Idempotent retry
* Concurrent duplicate requests
* Idempotency key conflict
* Exactly two ledger entries per successful transfer
* Failed transfers producing no financial side effects
* Concurrent debits against the same wallet
* Final balance correctness under concurrent transfers

Concurrency tests will use multiple goroutines issuing transfers against the same wallet and verify that no double spending occurs.

## 11. Assumptions and Tradeoffs

* PostgreSQL is used because the assignment explicitly prefers it and its transactional and row-locking capabilities fit the consistency requirements.
* Wallet balances are stored rather than recalculated from the ledger for every request.
* `amount` is represented as an integer smallest currency unit to avoid floating-point precision issues.
* The transfer workflow is synchronous; asynchronous workers or messaging are unnecessary for the assignment scope.
* Authentication and authorization are outside the assignment scope.

## Start service pre requisites:
export DATABASE_URL="postgres://wallet_user:wallet_password@localhost:5432/wallet_transfer?sslmode=disable"



## AI disclosure

1. What tool you used (Cursor, Claude Code, Antigratvity etc.)
--> AI Tool: ChatGPT — GPT-5.6 Sol
2. How you generally use the tool for your work.
--> Generally as I worked for financial application in past , I used it to only generate the system design diagrams.I avoid giving code base access directly.
3. A transcript of your entire session with your AI tool of choice. You can add this to the repo or email it to us with your submission. If for some reason, this is not possible, give us all the prompts that you used with the AI.
--> I do not have the complete transcript of it , here are the prompt that I used.
Initial schema design:
I’m working on the PostgreSQL design for a wallet transfer assignment and I want to validate my understanding before I start writing the actual SQL.
I’m thinking of keeping the design around four main tables: wallets, transfers, ledger_entries, and idempotency_records.
For wallets, I want to store the wallet ID and current balance, along with created_at and updated_at. I’m planning to use BIGINT for balance and transfer amount, storing money in the smallest currency unit instead of using float. I also want the database to make sure the balance can never go below zero.
For transfers, I want to store the source wallet, destination wallet, amount, status, and timestamps. The source and destination should be foreign keys to wallets. Amount should always be positive, and source and destination should not be the same wallet. The transfer status should only be PENDING, PROCESSED, or FAILED.
I also want to make sure we don’t treat two transfers as duplicates just because the wallets and amount are the same. For example, A to B for 100 with idempotency key abc and another A to B for 100 with idempotency key xyz should be treated as two separate valid transfers.
For the ledger, I want every successful transfer to create exactly one DEBIT entry for the source wallet and one CREDIT entry for the destination wallet. I’m thinking of using UNIQUE(transfer_id, entry_type) so we can’t accidentally create two debits or two credits for the same transfer. My understanding is that this only guarantees at most one of each, and the exactly-two guarantee should come from the database transaction.
For idempotency, I want a separate idempotency_records table because transfer and ledger history may be kept for a long time, while idempotency data could have a shorter retention period later.
I’m thinking of storing the idempotency key, a hash of the original request, transfer ID, response code, response body, created_at, and expires_at.
The behavior I want is: if the key is new, process the transfer. If the same key comes again with the same request, return the original result. If the same key is reused with a different request, return a conflict instead of processing it again.
For concurrent requests using the same idempotency key, I want to rely on PostgreSQL uniqueness and transaction behavior rather than doing a SELECT first and then an INSERT in Go, because that could introduce a race condition.
For the actual transfer flow, my current thought is to use one PostgreSQL transaction: claim the idempotency key, create a PENDING transfer, lock both wallet rows using SELECT FOR UPDATE, always lock them in a deterministic order, check the source balance, update both balances, insert the DEBIT and CREDIT ledger entries, mark the transfer as PROCESSED, store the response for replay, and then commit.
For insufficient funds or another business failure, I’m not fully sure yet whether it’s better to persist the transfer as FAILED and store that failure response for idempotent replay while keeping wallet and ledger changes at zero.
Can you review this design like a senior backend/database reviewer? Please point out anything unsafe around concurrency, idempotency, transaction boundaries, or data integrity, but keep the solution simple enough for this assignment.
Let’s start with the table relationships and which table should own the idempotency key. Don’t generate the final SQL yet.

Test script:
I want to create local bash scripts using curl to test the concurrency behaviour of my wallet transfer API.

The API is:

POST http://localhost:8080/transfers

Request body:

{
"idempotencyKey": "unique-key",
"fromWalletId": "source-wallet-id",
"toWalletId": "destination-wallet-id",
"amount": 100
}

Please create the scripts step by step based on the following scenarios.

1. Concurrent transfers with different idempotency keys

I want to send multiple requests at the same time from the same source wallet to the same destination wallet.

For example:

* Source wallet balance: 500
* Destination wallet balance: 500
* Number of concurrent requests: 10
* Amount per request: 100
* Every request should use a different idempotency key such as:

  * concurrent-1
  * concurrent-2
  * concurrent-3
  * etc.

Expected behaviour:

* Only 5 transfers should be successfully processed because the source wallet only has 500 available.
* The remaining 5 requests should fail with insufficient funds.
* The source wallet balance should never become negative.
* Successful requests should return HTTP 201.
* Insufficient-fund requests should return HTTP 422.
* The final source wallet balance should be 0.
* The destination wallet balance should become 1000.
* There should be exactly 5 DEBIT ledger entries and 5 CREDIT ledger entries.

The script should:

* Run all curl requests concurrently using background processes.
* Wait for all requests to finish before printing results.
* Store or print each response separately so I can see the HTTP status and response body for every request.

2. Concurrent duplicate requests using the same idempotency key

I also want a separate concurrency test for idempotency.

Send 10 requests concurrently with exactly the same:

* idempotency key
* source wallet
* destination wallet
* amount

For example:

* idempotency key: duplicate-concurrent-001
* amount: 100
* 10 simultaneous requests

Expected behaviour:

* The transfer should happen only once.
* The source wallet should be debited only once.
* The destination wallet should be credited only once.
* Only one transfer record should exist for this request.
* Exactly two ledger entries should exist:

  * one DEBIT
  * one CREDIT
* Every duplicate response should refer to the same transfer ID.
* Replayed responses should represent the same original result.

3. Concurrent requests using the same idempotency key but different payloads

I also want to test incorrect reuse of an idempotency key.

Send multiple concurrent requests using the same idempotency key, but change the amount in some requests.

For example:

Request A:

* key: conflict-concurrent-001
* amount: 100

Request B:

* key: conflict-concurrent-001
* amount: 200

Expected behaviour:

* Only the request that successfully claims the idempotency key should be processed.
* Requests using the same key with a different payload should receive HTTP 409 Conflict.
* No duplicate wallet balance changes or ledger entries should be created.

4. Concurrent transfers between the same two wallets in opposite directions

I want to verify that the deterministic wallet locking logic prevents deadlocks.

Send requests concurrently such as:

* Wallet A -> Wallet B
* Wallet B -> Wallet A

Use different idempotency keys for every request.

Expected behaviour:

* Requests should complete without deadlocking.
* Wallets should be locked in deterministic order internally.
* Final balances should remain consistent.
* Every processed transfer should have exactly one DEBIT and one CREDIT ledger entry.

5. Database verification after each test

After every concurrency script, give me PostgreSQL commands that I can run using docker compose to verify:

* wallet balances
* number of PROCESSED transfers
* number of FAILED transfers
* ledger entry count
* DEBIT total
* CREDIT total
* idempotency records
* whether duplicate transfer IDs were created

Also keep each scenario in a separate script so I can run and understand them individually.






Since the server is running on `:8080`, use these curls to verify the important cases.

### 1. Successful transfer

Assuming the test wallets are still:

```text
Source:      11111111-1111-1111-1111-111111111111
Destination: 22222222-2222-2222-2222-222222222222
```

Run:

```bash
curl -i -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "test-001",
    "fromWalletId": "11111111-1111-1111-1111-111111111111",
    "toWalletId": "22222222-2222-2222-2222-222222222222",
    "amount": 100
  }'
```

Expected:

```text
HTTP/1.1 201 Created

{"transferId":"...","status":"PROCESSED"}
```

### 2. Same request + same idempotency key

Run the **exact same curl again**:

```bash
curl -i -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "test-001",
    "fromWalletId": "11111111-1111-1111-1111-111111111111",
    "toWalletId": "22222222-2222-2222-2222-222222222222",
    "amount": 100
  }'
```

It should return the **same transfer ID**, and balances must **not change again**.

### 3. Same key but different request

Now deliberately reuse `test-001` but change the amount:

```bash
curl -i -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "test-001",
    "fromWalletId": "11111111-1111-1111-1111-111111111111",
    "toWalletId": "22222222-2222-2222-2222-222222222222",
    "amount": 200
  }'
```

Expected:

```text
HTTP/1.1 409 Conflict
```

Something like:

```json
{"error":"idempotency key reused with different request"}
```

### 4. Insufficient funds

Use a **new key** and an amount larger than the source balance:

```bash
curl -i -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "insufficient-001",
    "fromWalletId": "11111111-1111-1111-1111-111111111111",
    "toWalletId": "22222222-2222-2222-2222-222222222222",
    "amount": 999999
  }'
```

Expected:

```text
HTTP/1.1 422 Unprocessable Entity
```

with a `FAILED` transfer.

Balances should remain unchanged and there should be **no ledger entries for this failed transfer**.

### 5. Same source and destination

```bash
curl -i -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "same-wallet-001",
    "fromWalletId": "11111111-1111-1111-1111-111111111111",
    "toWalletId": "11111111-1111-1111-1111-111111111111",
    "amount": 100
  }'
```

Expected:

```text
HTTP/1.1 400 Bad Request
```

### 6. Verify the database after testing

Balances:

```bash
docker compose exec postgres psql \
  -U wallet_user \
  -d wallet_transfer \
  -c "SELECT id, balance FROM wallets ORDER BY id;"
```

Transfers:

```bash
docker compose exec postgres psql \
  -U wallet_user \
  -d wallet_transfer \
  -c "SELECT id, amount, status FROM transfers ORDER BY created_at;"
```

Ledger:

```bash
docker compose exec postgres psql \
  -U wallet_user \
  -d wallet_transfer \
  -c "SELECT transfer_id, wallet_id, entry_type, amount FROM ledger_entries ORDER BY created_at;"
```

Idempotency:

```bash
docker compose exec postgres psql \
  -U wallet_user \
  -d wallet_transfer \
  -c "SELECT idempotency_key, transfer_id, response_code FROM idempotency_records ORDER BY created_at;"
```


## Concurrent testing
chmod +x tests/concurrent_trasfers.sh 
. tests/concurrent_trasfers.sh

concurrent_transfer script:


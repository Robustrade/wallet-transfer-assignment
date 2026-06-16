# Wallet Transfer Service - Design Note

## Understanding of the Problem

The goal is to build a wallet-to-wallet transfer service using Spring Boot and PostgreSQL.

The service must safely transfer funds between wallets while maintaining transactional consistency, preventing double spending, and supporting client retries through idempotency.

The key requirements are:

* Atomic debit and credit operations
* Exactly-once behavior for repeated requests using the same idempotency key
* Double-entry ledger recording for auditability
* Safe handling of concurrent debits from the same wallet
* Consistent wallet balances under all failure scenarios

---

## API Contract

### POST /transfers

Request

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

Successful Response

```json
{
  "transferId": "trf_123",
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100,
  "state": "PROCESSED"
}
```

Expected Errors

* 400 - Validation failure
* 404 - Wallet not found
* 409 - Idempotency key reused with different payload
* 422 - Insufficient funds

### Supporting Endpoints

#### POST /wallets

Creates a wallet with an initial balance for testing.

#### GET /wallets/{walletId}

Returns the current wallet balance.

---

## Domain Model

The solution is built around four core entities.

### Wallet

Represents a balance-holding account.

Fields:

* wallet_id
* balance
* created_at
* updated_at

Balances are stored directly on the wallet record for efficient reads.

### Transfer

Represents a money movement request and its processing state.

Fields:

* transfer_id
* idempotency_key
* from_wallet_id
* to_wallet_id
* amount
* state
* failure_reason

A transfer serves as the primary business record for the operation.

### Ledger Entry

Represents an immutable accounting event associated with a transfer.

Each successful transfer generates:

* one DEBIT entry
* one CREDIT entry

The ledger provides a complete audit trail independent of wallet balances.

### Idempotency Record

Stores request fingerprints and previously generated responses.

This entity ensures that retries do not create duplicate transfers or duplicate side effects.

---

## Database Design

### wallets

Stores current wallet balances.

| Column     | Description                 |
| ---------- | --------------------------- |
| wallet_id  | Unique wallet identifier    |
| balance    | Current wallet balance      |
| created_at | Creation timestamp          |
| updated_at | Last modification timestamp |

Constraints:

* Primary key on wallet_id
* balance >= 0

### transfers

Stores transfer requests and processing outcomes.

| Column          | Description                     |
| --------------- | ------------------------------- |
| transfer_id     | Unique transfer identifier      |
| idempotency_key | Client supplied idempotency key |
| from_wallet_id  | Source wallet                   |
| to_wallet_id    | Destination wallet              |
| amount          | Transfer amount                 |
| state           | PENDING, PROCESSED, FAILED      |
| failure_reason  | Optional failure details        |
| created_at      | Creation timestamp              |
| updated_at      | Last modification timestamp     |

Constraints:

* Unique idempotency_key
* amount > 0
* Source and destination wallets must be different
* Foreign key validation against wallets

Indexes:

* from_wallet_id
* to_wallet_id

### ledger_entries

Stores immutable accounting records.

| Column      | Description             |
| ----------- | ----------------------- |
| entry_id    | Ledger entry identifier |
| wallet_id   | Associated wallet       |
| transfer_id | Associated transfer     |
| entry_type  | DEBIT or CREDIT         |
| amount      | Entry amount            |
| created_at  | Creation timestamp      |

Constraints:

* amount > 0
* Unique (transfer_id, wallet_id, entry_type)

Indexes:

* transfer_id
* wallet_id

This uniqueness constraint prevents duplicate ledger entries from being created for the same transfer.

### idempotency_records

Stores previously processed request information.

| Column          | Description                 |
| --------------- | --------------------------- |
| idempotency_key | Unique idempotency key      |
| request_hash    | Hash of transfer payload    |
| transfer_id     | Associated transfer         |
| http_status     | Stored HTTP response status |
| response_body   | Serialized response payload |
| created_at      | Creation timestamp          |

The request hash is used to detect when the same idempotency key is reused with a different request payload.

---

## Transfer Lifecycle

A transfer follows the state transitions below:

```text
PENDING -> PROCESSED
PENDING -> FAILED
```

Processing flow:

1. Reserve the idempotency key.
2. Create a transfer record in PENDING state.
3. Validate source and destination wallets.
4. Perform a conditional debit on the source wallet.
5. Credit the destination wallet.
6. Create DEBIT and CREDIT ledger entries.
7. Mark the transfer as PROCESSED.
8. Persist the final response in the idempotency record.

If validation or business rules fail:

1. Transfer state is updated to FAILED.
2. Failure reason is recorded.
3. No ledger entries are created.
4. No balance changes are committed.

All steps execute within a single database transaction.

---

## Idempotency Strategy

The idempotency key is unique across transfer requests.

When a request arrives:

1. A deterministic request hash is generated from the transfer payload.
2. An idempotency record is inserted to reserve the key.
3. If the key already exists:

    * If the stored hash matches, the original response is returned.
    * If the stored hash differs, the request is rejected with HTTP 409.

After processing completes, the final HTTP status and serialized response are stored in the idempotency record.

This guarantees that repeated requests do not execute transfer logic multiple times.

---

## Concurrency Strategy

The primary concurrency concern is preventing multiple simultaneous debits from overspending the same wallet.

I considered locking wallet rows using `SELECT FOR UPDATE`. Instead, I chose a transactional conditional debit because PostgreSQL already provides row-level locking during UPDATE operations.

The source wallet balance is updated using:

```sql
UPDATE wallets
SET balance = balance - :amount
WHERE wallet_id = :walletId
  AND balance >= :amount;
```

If the update affects zero rows, the wallet either does not exist or does not have sufficient funds.

When concurrent transfers target the same wallet, PostgreSQL serializes updates on the row. Once the first transaction commits, waiting transactions re-evaluate the balance condition against the latest committed value.

This prevents double spending without requiring application-level locks or distributed synchronization.

---

## Consistency Rules

The following conditions must always hold:

* Wallet balances must never become negative.
* A successful transfer must create exactly two ledger entries.
* Debit and credit amounts must be equal.
* Transfer processing must be atomic.
* Ledger entries and wallet balances must remain consistent.

---

## Failure Handling

### Missing Wallet

Result:

* Transfer marked FAILED
* No ledger entries created
* No balance changes committed

### Insufficient Funds

Result:

* Transfer marked FAILED
* No ledger entries created
* No balance changes committed

### Duplicate Request (Same Payload)

Result:

* Original response returned
* No additional side effects

### Duplicate Request (Different Payload)

Result:

* Request rejected with HTTP 409

---

## Observability

To make transfer processing easier to debug and audit:

* Transfer identifiers are included in logs.
* Error responses contain stable error codes.
* Transfer state is persisted.
* Health endpoints are exposed through Spring Boot Actuator.

Persisted transfer records and ledger entries make successful and failed transfers inspectable after execution.

---

## Testing Approach

Integration tests verify:

* Successful transfer execution
* Wallet-not-found handling
* Insufficient funds handling
* Duplicate idempotency requests
* Ledger balancing
* Concurrent debit attempts against the same wallet

Tests validate both API responses and resulting database state to ensure correctness of the complete workflow.


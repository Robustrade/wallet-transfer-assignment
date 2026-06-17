# Wallet Transfer Service - Design Approach

## Problem Understanding

Build a wallet-to-wallet transfer service that supports:

- Money transfer between wallets
- Idempotent requests
- Double-entry ledger
- Safe concurrent execution
- Correct wallet balances

---

## Database Design

### Wallet

Stores current balance.

Fields:
- walletId
- balance

### Transfer

Stores transfer information.

Fields:
- transferId
- idempotencyKey
- fromWalletId
- toWalletId
- amount
- status

Status:
- PENDING
- PROCESSED
- FAILED

### LedgerEntry

Stores audit records.

Fields:
- ledgerEntryId
- transferId
- walletId
- type (DEBIT/CREDIT)
- amount

---

## Idempotency Strategy

Every request contains an idempotencyKey.

Before creating a transfer:

1. Check if a transfer already exists with the same key.
2. If found, return the existing transfer.
3. Otherwise create a new transfer.

A unique constraint will be added on idempotencyKey.

This prevents duplicate transfers.

---

## Concurrency Strategy

To prevent double spending, wallet rows will be locked during transfer execution.

I will use:

- Database transaction
- Pessimistic row-level locking

This ensures only one transfer can update a wallet balance at a time.

---

## Transfer Flow

1. Validate request
2. Check idempotency key
3. Start transaction
4. Lock source wallet
5. Lock destination wallet
6. Validate balance
7. Create transfer (PENDING)
8. Debit source wallet
9. Credit destination wallet
10. Create DEBIT ledger entry
11. Create CREDIT ledger entry
12. Mark transfer PROCESSED
13. Commit transaction

---

## Failure Handling

If any step fails:

- Transaction is rolled back
- Transfer marked FAILED
- Wallet balances remain unchanged

---

## Testing Plan

1. Successful transfer
2. Insufficient balance
3. Duplicate idempotency request
4. Ledger entry validation
5. Concurrent transfer execution

---

## Assumptions

- PostgreSQL database
- Wallets are pre-created
- Amount is always positive
- Single application instance
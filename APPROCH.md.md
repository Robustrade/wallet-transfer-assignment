# Wallet Transfer Service — First Flow

## Problem Statement

Implement a service that supports **wallet transfers**.

Transfers must guarantee:

- **Idempotent request handling**
- **Double-entry ledger recording**
- **Correct balance tracking**
- **Safe concurrent execution**

The API should provide **exactly-once semantics at the API level** when an `idempotencyKey` is provided.

---

## Functional Requirements

1.Build an application that transfers the amount 
from one wallet to another wallet
2.api to fetch the balance(either from the ledger entry or the balance bucket)
3.Transfer api history

## Non-Functional Requirements
1.Consistency over availability
2.Auditabilty(ledger entry)
3.Performace(consistency > availabilty : cannot lock the things for long time, 
define the acceptable latency bound)
4.Idempotency: same request cannot be processed multiple times
5.retry mechanism

---

## Expected behavior
### 1.Concurrency Control and Double-Deduction Prevention
    Inorder to make the system consistent going with the pessmistic locking and to implement pessimistic locking we have to aquire the lock for two wallets in the order to avoid deadlock. 

### 2. Absolute Idempotency Lifecycle
    Must expose idempotency at the entry point of the service boundray and check if the key already exists in the table.
    -> if there is a key
        Completed request then return the same resonse
        PROCESSING request conflit, then return the request is already in progress(have to handle the server crash processing request)
        FAILED request, then process the request and mark the status as processing
    ->no KEY process the request

### 3.Ledger Integrity
    Every successfull transation must make 2 entry in the ledger, one will be with the credit and other with the debit

Every transfer must produce **two ledger entries**.

| entry_id | wallet_id | transfer_id | type   | amount |
|----------|-----------|-------------|--------|--------|
| 1        | wallet_1  | T1          | DEBIT  | 100    |
| 2        | wallet_2  | T1          | CREDIT | 100    |

Rules:

- every transfer must generate exactly two entries
- debit from source wallet
- credit to destination wallet
- ledger must always balance

### API or event contract

POST /wallet
request:
{
    idempotencyKey:String,
    name:String,
    mobile:String,
    email:String
}
response:
{
    name:String,
    walletId:String,
    amount:double,
    createdAt:date
}


POST /transfer
request:
{
  idempotencyKey: String,
  fromWalletId: String,
  toWalletId: String,
  amount: Double
}
response:
{
    date:Date,
    amount:double,
    toWallet:double,
    transactionId:int
}

GET /wallet/{walletId}
response:
{
    availableBalance:double,
    walletId:int
}

GET /wallets/{walletId}/history?Month=APR&Category=MONEYSENT&paymentStatus=SUCCESSFULL
{
    toWallet:int,
    amount:double,
    date:Date
}


### side effects


### failure modes

---
## idempotency behaviour

---
## retry behaviour

---
## consistency expectations

---
## observability expectations

---

## testing strategy

---


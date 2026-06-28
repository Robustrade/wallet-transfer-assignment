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

-1.Build an application that transfers the amount 
-from one wallet to another wallet
-2.api to fetch the balance(either from the ledger entry or the balance bucket)
-3.Transfer api history

## Non-Functional Requirements
1. Consistency over availability
2. Auditability (ledger entries)
3. Performance (consistency > availability; avoid holding locks for long; define acceptable latency bounds)
4. Idempotency: the same request cannot be processed multiple times
5. Retry mechanism

---

## Expected behavior
### 1.Concurrency Control and Double-Deduction Prevention
### 2. Absolute Idempotency Lifecycle
### 3.Ledger Integrity
    Inorder to make the system consistent going with the pessmistic locking and to implement pessimistic locking we have to aquire the lock for two wallets in the order to avoid deadlock. 

### 2. Absolute Idempotency Lifecycle
    -Must expose idempotency at the entry point of the service boundray and check if the key already exists in the table.
    -if there is a key
        Completed request then return the same resonse
        PROCESSING request conflit, then return the request is already in progress(have to handle the server crash processing request)
        FAILED request, then process the request and mark the status as processing
    -no KEY process the request

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
``` json
{
    "idempotencyKey":"abc123",
    "name":"tharun",
    "mobile":"91021029102",
    "email":"tharunreddyy48@gmail.com"
}
```
response:
``` json
{
    "name":"tharun",
    "walletId":1001,
    "amount":200.00,
    "createdAt":"2026 Jun 25 18:53"
}
```


POST /transfer
request:
``` json
{
  "idempotencyKey": "abc012",
  "fromWalletId": 1001,
  "toWalletId": "acha12b",
  "amount": 400.00
}
```

``` json
response:
{
    "date":"2026 Jun 25 18:53",
    "amount":400.00,
    "toWallet":"akad901",
    "transactionId":9001
}
```

GET /wallet/{walletId}
response:
``` json
{
    "availableBalance":800.01,
    "walletId":9001
}
```

GET /wallets/{walletId}/history?Month=APR&Category=MONEYSENT&paymentStatus=SUCCESSFULL
``` json
{
    "toWallet":2001,
    "amount":300.00,
    "date":"2026 Jun 25 18:53"
}
```


### side effects
``` text
While database-level pessimistic locking (SELECT ... FOR UPDATE) guarantees absolute consistency (CP) by creating a strict serialization boundary around wallet records, it introduces a severe operational trade-off regarding system latency. Because a pessimistic lock forces all concurrent transaction threads targeting the same walletId to queue up sequentially, the system's throughput becomes entirely bound by the execution time of the longest transaction in the queue.
```

---
### failure modes

---
## idempotency behaviour
-Must expose idempotency at the entry point of the service boundray and check if the key already exists in the table.
    -if there is a key
        Completed request then return the same resonse
        PROCESSING request conflit, then return the request is already in progress(have to handle the server crash processing request)
        FAILED request, then process the request and mark the status as processing
    -no KEY process the request

---
## retry behaviour
-In a distributed setup, your application servers talk to your database cluster over a network network.
-Under heavy load, your application's database connection pool (e.g., HikariCP) runs out of available connections, causing a connection timeout before your code can even send the query. We can retry under this scenario.

---
## consistency expectations
-To guarantee absolute data integrity and prevent any instance of double-spending or balance mismatches, the application strictly adheres to a Strong Consistency (CP) model by choosing the pessimistic locking. 
-The system enforces consistency by calculating balances on the fly directly from an immutable ledger under the protection of strict database-level locks.

---
## observability expectations
-In the highly concurrent environment we have to log the worst case reponse time of the request. 
-For example if 10 request are fired to deduct the amount from the same wallet, then how long it will take the 10 request to complete the transaction.
-have to monitor the idempotency tables to know when the service will be chocking in high concurrency.


---

## testing strategy

## 1.Happy Path
-create wallet, make a transfer and making sure the balances tally with the amount in the bucket.
-Along with this we will test for the idempotency mechanism, we will fire two request with the same idempotency key and have to check the first one executes and second one will return the cached respone.

## Concurrency and Race Condition(Stress Testing)
-Simulte the 20+ concurrent request at the same time for deducting the amount from the same account and verify that the balance will not go below zero.
-Sum of the request will fail with the LockTimeoutException and the retry mechanism has to catch this exception and try to retry the same request.

## Chaos Engineering (Infrastructure Failure Testing)
-Pulling down the database server in the middle of the transaction and to look for if the database clealy rolls back everything and there should be no partial entries in the database.

---


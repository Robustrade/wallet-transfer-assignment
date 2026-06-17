# Flow of Transactions

1. **Check idempotency** ➔ if exists then return the status of transactions
2. **If not:**
    * Create transactions
    * Lock wallets ➔ for both sender and receiver
        * > **Most Important:** Lock wallet in increasing ID of `wallet_id`, otherwise deadlock can occur.
    * Create transfer entry in DB ➔ with pending status
    * Transfer money from user1 account
    * Insert ledger debit
    * Transfer money from user2 (to user) wallet
    * Insert ledger credit
    * Update transfer with successfully
    * Create idempotency with successful status
    * Committed
    * Otherwise rollback if error

---

# Database Choice

This is a money transfer app so we need to ensure ACID compliance. So we choose **PostgreSQL**.

* `wallet`, `transfer`, `ledger_entries`, and `idempotency_records` all are in Postgres.
* **Foreign keys:**
    * `wallets(wallet_id)` for `transfers`
    * `wallets(wallet_id)` for `ledger_entries`

### Indexes
Add proper indexes on:
* `transfers(from_wallet_id)`
* `transfers(to_wallet_id)`

---

# Transaction Safety

* When locking wallets ➔ lock ID in increasing order.

---

# Test Cases

* **10 concurrent** ➔ 100% successful
* **50 concurrent** ➔ 100% successful, time ==> 1.0127165s
* **100 concurrent** ➔ 100% successful, time ==> 1.4835776s or 2.2954332s
* **200 concurrent** ➔ 100% successful, time ==> 2.3527446s
* **400 concurrent** (1 millisecond gap) ➔ 100% successful, time ==> 2.9907291s
* **1000 concurrent** (1 millisecond gap) ➔ 100% successful, time ==> 6.625749s

> **DEBIT == CREDIT for all cases**
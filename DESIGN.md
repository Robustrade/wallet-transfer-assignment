1. Problem Statement - Functional Requirement
As per my understanding, have to create a wallet-to-wallet transfer service with the following endpoint POST /transfers with few key requirements
    1. Exactly once behaviour meaning a transaction should execute only once, this has to be implemented through the use of an idempotency key which is the mechanism being used for a lot of scenarios of this type.
    2. Double-entry ledger meaning credit and debit both should be happening in one transaction, debit from sender, credit to receiver.
    3. Correct wallet balances in concurrent situations - during concurrency i.e 4-5 transactions at the same time , need to make sure there are no race conditions, might use mutex for it.
    4. A safe transfer state machine (PENDING -> PROCESSED/FAILED)

2. API contract
POST /transfers

Request:

json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}

Response (200 / 201):

json
{
  "transactionId": "uuid",
  "state": "PROCESSED",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100,
  "createdAt": "..."
}

Error cases:

400 — invalid payload (missing fields, non-positive amount, same wallet twice)
404 — wallet does not exist
409 — Idempotency key reused with a different payload
200/201 — Returning response from idempotent stored successfully result from a prior successful result for a repeated idempotency key

3. Database Design
Tables: wallets, transfers, ledger_entries, idempotency_records as per requirement.
Will be using Postgresql for DB

Key constraints:

unique index on transfers->idempotency_key
unique constraint on ledger_entries (transfer_id, wallet_id, type)
wallets->balance >= 0, amount > 0 checks

Schema is version-controlled at db/init/001_schema.sql and applied automatically
by `docker compose up -d` (see README "Solution: Running Locally"). This
replaced a manually-created local DB so the schema is actually reproducible
from the repo instead of only existing on one machine.

4. How to implement idempotency
When a request first comes, I will look it up in the idempotency_records table. If found, we can return it right then and there. If not found I will proceed with
the transaction and then add it there at the end of the transaction.
Will make a request hash from the payload and compare it too if it doesn't match then will return 409 conflict.

5. Concurrency strategy
Can Lock both wallet rows with SELECT.. FOR UPDATE, always in consistent ordering to prevent deadlocks between two transfers that are accessing the same resource
All transactions i.e balance checks, balance updates, ledger inserts, and state transitions happen inside one transaction and then commit only after all steps succeed. In this way we ensure atomicity and consistency.
Isolation level: READ COMMITTED (default in postgres) is sufficient given the current requirement for now.

Found during implementation: consistent lock ordering on the two explicit
SELECT ... FOR UPDATE calls wasn't sufficient on its own. transfers.from_wallet_id
and to_wallet_id are FK-constrained to wallets(id), so inserting the transfer
row implicitly takes a shared FOR KEY SHARE lock on both wallet rows. With the
insert running before the explicit locks, concurrent transfers on the same
wallet pair could each hold that shared lock and then race to upgrade to
FOR UPDATE, which Postgres reports as a genuine deadlock (40P01) and resolves
by aborting one of them. Fixed by acquiring the FOR UPDATE locks before
inserting the transfer row — a transaction never blocks on a lock it already
holds, so the FK check on the insert is satisfied for free instead of racing.

6. Architecture - LLD 
Going to use handler, service, repository pattern, this make sures everything is loosely coupled and new features can be added without much modification and regression. 
Handler will handle api side of things i.e request and response
Service will handle business logic
repository will handle database side of things.

7. Unit tests will be included.


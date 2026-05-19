What concepts we are going to implement and how
## Idempotency
This is to prevent duplicate entry for same transaction. For example if a user A sends 50 to user B wallet then it should only deduct money once and does not duplicate it by deducting twice/thrice. This behaviour can be implemented using UUID for specific transaction session.

## Concurrency
This is to prevent processing two request at the same time. This can be prevented using database locking mechanism. We going with pessimistic locking so that race condition is avoided.

We will lock the row until first transaction/request is completed.

## Ledger consistency
Money cannot be created or destroyed out of thin air. Every debit must have a corresponding credit. Every request to transfer money will have to step process logged in DB. First debit from user A and Credit to user B.

## Safe State Transitions 
Safe State Transitions ensure the transaction moves step-by-step through the lifecycle, leaving a perfect audit trail if something goes wrong.


## Design schema
ledger entries
| entry_id | wallet_id | transfer_id | type   | amount | created_at |

composite primary key wallet_id, transfer_id 
index on wallet_id
index on transfer_id
FOREIGN KEY (transfer_id) REFERENCES transfers(transfer_id),
    FOREIGN KEY (wallet_id) REFERENCES wallets(wallet_id)

Wallet
| wallet_id | user_id | balance | currency |  created_at
FOREIGN KEY (user_id)


wallet_id primary_key
user_id foreign key from users table
Transfers
| transfer_id       |source_wallet_id| target_wallet_id | amount | currency| state     | created_at | updated_at
 index on (source_wallet_id, target_wallet_id)

idempotency_records
| idempotency_key       | transfer_id | request_hash    | response_status | resoponse_body| created_at

idempotency_key primary key


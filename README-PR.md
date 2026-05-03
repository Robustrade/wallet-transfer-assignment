# Run
go mod tidy
golangci-lint run
go run cmd/server/main.go

# created DB wallet
CREATE DATABASE wallet;
CREATE USER wallet_user WITH PASSWORD 'wallet_pass';
GRANT ALL PRIVILEGES ON DATABASE wallet TO wallet_user;
\c wallet

# create tables
wallet-transfer-assignment/migrations -> psql -U postgres -d wallet -f 001_init.sql

# Grant permission
GRANT ALL PRIVILEGES ON TABLE wallets TO wallet_user;
GRANT ALL PRIVILEGES ON TABLE transfers TO wallet_user;
GRANT ALL PRIVILEGES ON TABLE ledger_entries TO wallet_user;
GRANT ALL PRIVILEGES ON TABLE idempotency_records TO wallet_user;
GRANT USAGE, SELECT ON SEQUENCE ledger_entries_id_seq TO wallet_user;

# Seed Wallets
INSERT INTO wallets (id, balance) VALUES ('wallet_1', 1000);
INSERT INTO wallets (id, balance) VALUES ('wallet_2', 500);

# Curl
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey":"txn-1",
    "fromWalletId":"wallet_1",
    "toWalletId":"wallet_2",
    "amount":100
  }'

# run test
go test -v -count=1 ./...

------------------------------------------------------------------------

# PR Notes

1. Problem Statement

Implement a wallet-to-wallet transfer service ensuring:
•	idempotency 
•	atomic execution 
•	ledger consistency 
•	safe concurrent handling 

2. Expected Behaviour

•	A transfer moves funds from one wallet to another 
•	Exactly one transfer should be executed per idempotency key 
•	Duplicate requests return the same response 
•	Transfers succeed only if sufficient balance exists 

3. API Contract

POST /transfers
  Request:
  {
    "idempotencyKey": "string",
    "fromWalletId": "string",
    "toWalletId": "string",
    "amount": float
  }
  Response:
    Success:
    {
        "transfer_id": "9b7d5a15-f749-4c7f-9d4a-44beade9276a",
        "status": "PROCESSED",
        "message": "transfer completed successfully"
    }

    Failure:
    {
        "error": {
            "code": "VALIDATION_ERROR",
            "message": "cannot transfer to same wallet"
        }
    }

4. Side Effects

•	Creates a transfer record 
•	Creates two ledger entries (DEBIT, CREDIT) 
•	Updates wallet balances 
•	Stores idempotency response 

5. Failure Modes

•	insufficient funds → FAILED transfer 
•	duplicate request → return stored response 
•	DB error → transaction rollback 
•	invalid input → request rejected 

6. Idempotency Behaviour

•	idempotency key stored with UNIQUE constraint 
•	First request inserts record 
•	Subsequent requests return stored response 
•	Prevents duplicate execution 

7. Retry Behaviour

•	Safe retries supported via idempotency 
•	Duplicate requests do not re-execute transfer 
•	In-progress requests may return “processing” (optional handling) 

8. Consistency Expectations

•	All operations run in a single database transaction 
•	Ledger entries and balance updates are atomic 
•	Row-level locks prevent concurrent balance corruption 

9. Observability (basic)

•	Errors returned to client 
•	Logs can be added for: 
  o	transfer creation 
  o	failures 
  o	retries 

10. Testing Strategy

•	Unit/integration tests for: 
  o	successful transfer 
  o	insufficient balance 
  o	idempotency behaviour 
  o	concurrent transfers 
•	Validate: 
  o	balance correctness 
  o	ledger consistency 
  o	no duplicate transfers 

11. Idempotency Strategy

The system ensures idempotent behaviour using a dedicated idempotency_records table.
The idempotency implementation stores:
•	idempotency key (unique) 
•	request payload (JSONB) 
•	request hash 
•	response 

Behaviour:
•	First request → stored and processed 
•	Duplicate request with same payload → returns stored response 
•	Duplicate request with different payload → rejected with conflict error 

This ensures:
•	correctness under retries 
•	prevention of silent mismatches 
•	full auditability of requests

12. Distributed System Considerations

The system is designed to behave correctly under retries, duplicate requests, and failures:
•	Duplicate Requests & Retries
Handled via idempotency keys, ensuring that repeated requests return the same response without re-executing the transfer. 
•	Transactional Safety
All operations (transfer creation, ledger entries, balance updates, idempotency response) are executed within a single database transaction, preventing partial execution. 
•	Failure Handling
On any error, the transaction is rolled back, ensuring no inconsistent state. Failed transfers are explicitly recorded. 
•	State Consistency
Transfer states follow a controlled lifecycle (PENDING → PROCESSED / FAILED), ensuring safe transitions under retries. 
This design ensures correctness under duplicate delivery, network failures, and concurrent execution.


13. Testing Strategy

The system is validated using behavioural tests via the API layer:
•	Concurrency Test
Simulates parallel transfers to ensure no double spending and correct balance updates. 
•	Idempotency Test
Ensures duplicate requests with the same idempotency key do not create multiple transfers. 
•	Ledger Validation
Verifies that total debits equal total credits. 
•	Failure Scenario Test
Validates insufficient balance handling and correct FAILED state persistence. 
Tests interact with the system via HTTP and verify database state, focusing on correctness rather than implementation details.

14. Testing Approach (Red → Blue → Green)

•	Red: Wrote failing tests for idempotency and concurrency scenarios 
•	Blue: Implemented minimal logic (idempotency key + row-level locking) to make tests pass 
•	Green: Refactored into clean layered architecture while keeping tests passing

15. AI Usage

I used ChatGPT as a supporting tool for validating design decisions (idempotency, concurrency control, transaction), refining edge cases, and improving logging and error handling.
I primarily used it as a reviewer to sanity-check approaches rather than generate the full solution.

Prompts used (summary):
idempotency handling strategies in transactional systems
logging and observability improvements






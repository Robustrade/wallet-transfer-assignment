# AI Usage

## AI Tool Used

ChatGPT

## Purpose of AI Assistance

ChatGPT was used as a development assistance and review tool during the implementation of the Wallet Transfer Service.

AI assistance was used for:

- Understanding Go and PostgreSQL concepts relevant to the assignment.
- Understanding transaction boundaries and rollback/commit behavior.
- Discussing PostgreSQL row-level locking and concurrent transfers.
- Understanding and reviewing the idempotency implementation.
- Debugging PostgreSQL and application errors encountered during development.
- Reviewing HTTP error handling and status-code mapping.
- Designing and reviewing integration-test scenarios.
- Reviewing the implementation against the assignment requirements.
- Preparing and reviewing project documentation.

## Key Prompts / Topics Used

The following are the main prompts/topics used during development:

### Application and API

- How should the wallet transfer API be implemented?
- How should the transfer handler and service interact?
- How should HTTP errors such as insufficient funds and wallet-not-found be returned?

### PostgreSQL Transactions

- How does `sql.Tx` work in Go?
- What happens when an error occurs inside a transaction?
- When should `tx.Commit()` and rollback be used?
- How should transfer creation, wallet updates, and ledger entries be handled atomically?

### Concurrency

- How does `SELECT FOR UPDATE` provide row-level locking?
- Why should wallets be locked in deterministic order?
- How can concurrent wallet transfers cause double spending?
- How can concurrent transfers be tested?

### Idempotency

- How should an idempotency key be stored?
- How should duplicate requests be handled?
- How can idempotency prevent duplicate transfers and ledger entries?

### Ledger

- How should double-entry ledger entries be created?
- Why should debit and credit entries be created in the same transaction as wallet balance updates?

### Debugging

AI assistance was also used to investigate errors encountered while running the application and integration tests, including:

- PostgreSQL UUID errors
- Missing database relations
- Migration path/configuration issues
- Foreign-key constraint errors
- HTTP 400/404/500 response handling
- Integration-test failures

### Testing

AI assistance was used to discuss and review integration tests covering:

- Successful transfer
- Idempotency
- Ledger correctness
- Insufficient funds
- Wallet not found
- Concurrent transfers

## Verification

AI suggestions were reviewed against the project implementation and tested locally.

The final implementation was verified using the project's test suite and static checks.

The developer remains responsible for the final implementation, configuration, testing and submission.
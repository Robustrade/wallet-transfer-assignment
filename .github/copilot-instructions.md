# Copilot Review Instructions

Review pull requests in this repository as if you are a senior backend engineer evaluating a coding assignment for a wallet transfer service.

Focus on:

- correctness before cleverness
- transactional safety
- idempotency guarantees
- concurrency handling for debits on the same wallet
- exactly-once semantics at the API level
- ledger consistency and double-entry correctness
- clean separation between handler, service, repository, and model layers
- test quality and coverage of critical behaviors
- clear error handling and safe state transitions

When reviewing pull requests:

- flag missing or weak uniqueness constraints for idempotency
- flag unsafe transaction boundaries
- flag balance updates that can race under concurrent writes
- flag missing ledger entries or unbalanced debit and credit records
- flag handlers that contain business logic
- flag repositories that embed workflow decisions
- flag tests that assert implementation detail instead of behavior
- suggest simpler designs when the solution is overengineered

Prefer actionable comments. Point out correctness, reliability, and maintainability concerns before style preferences.

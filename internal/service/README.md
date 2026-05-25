# Service Layer

Transfer business logic belongs here.

Responsibilities:

- enforce idempotency behavior
- orchestrate wallet locking, balance changes, transfer states, and ledger writes
- define transactional boundaries
- return stable results for duplicate idempotency keys

# Architecture

The service is a feature-first modular monolith:

```text
HTTP -> handler -> application service -> repository -> PostgreSQL
```

The `wallet` feature owns wallet creation, balances, and wallet history. The `transfer` feature owns request validation, durable idempotency, locking, balance movement, and transfer reads. Ledger entries and idempotency records are written by the transfer repository inside the same database transaction because they are part of one money movement.

Handlers decode/encode HTTP and map errors. Services validate and orchestrate through small repository interfaces. Repositories contain parameterized PostgreSQL persistence. Platform packages contain configuration, database opening, health, and HTTP setup.

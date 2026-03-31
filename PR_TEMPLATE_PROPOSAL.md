## Summary
Implemented a robust wallet transfer microservice prioritizing database-level strict concurrency safety, exactly-once transfer idempotency, and high scalability through optimized PostgreSQL connection pooling and Go-native minimal memory footprints.

## AI disclosure
Anthropic/Gemini was utilized as a pair-programming partner during development.
**Prompts Used:**
1. "Guide me through end-to-end setting up a production-ready clean architecture in Go for the wallet transfer assignment."
2. "Design PostgreSQL schemas with proper uniquely constrained idempotency logic and foreign keys."
3. "Generate rigorous concurrency test suites mimicking aggressive double-spend and exact-time concurrency idempotency clashes."
4. "Implement native Go connection pooling and Benchmark tests for scalability."

## Schema Design
- `wallets`: Tracks current balances stored natively for high-speed zero-aggregation reads.
- `transfers`: Maintains the ledger transfer state. Uses a `UNIQUE` index constraint on `idempotency_key` ensuring duplicate requests physically cannot duplicate state entries.
- `ledger_entries`: A dual-entry strictly enforcing two rows (DEBIT and CREDIT) per successful transfer.

## Idempotency Strategy
The strategy relies on a multi-layer defense mechanism:
1. **Cache Read**: Service explicitly checks the `transfers` table for an existing `idempotency_key`. If matched, returns identical response instantly.
2. **Database Constraint (Concurrency Defense)**: At the highest concurrent scale, two identical requests passing step 1 will hit standard `INSERT` evaluation. PostgreSQL’s `UNIQUE` constraint guarantees one fails gracefully (`23505 unique_violation`).

## Concurrency Strategy
- Handled at the database transaction layer using standard ACID guarantees with `ReadCommitted` isolation.
- Utilizes precise pessimistic row-level locking (**`SELECT ... FOR UPDATE`**) preventing read-then-write double-spending races.
- **Deadlock Immunity**: Wallet targets are dynamically ordered alphabetically before row locks are grabbed. Simultaneous cross-transfers (A -> B and B -> A) queue perfectly instead of deadlocking.

## How to Run
- Fast start via `docker-compose up --build -d` for seamless isolated startup.
- Locally via `go run cmd/server/main.go` exporting the `DATABASE_URL`.

## How to Test
- `go test -v ./...` executes robust table-driven unit tests.
- Contains massive scale concurrency simulations (`transfer_integration_test.go`) firing 20 simultaneous background `goroutines` against instances to mathematically prove db locking stability.
- `go test -bench . ./internal/service` achieves massive scale zero-allocation routing.

## Tradeoffs / Assumptions
- Maintaining explicit integers on the `wallets` table trades a slight write-penalty (an extra row update) for massive zero-calculation read speeds.
- PostgreSQL pool logic explicitly limits peak overhead RAM.

## Checklist

- [x] Tests pass
- [x] Lint passes
- [x] Format check passes
- [x] README or notes updated
- [x] PR description explains schema, idempotency, and concurrency

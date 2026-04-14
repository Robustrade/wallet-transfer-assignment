## Summary

Implemented a production-ready, highly scalable wallet transfer service in Go. The architecture goes beyond standard requirements by structurally enforcing the **Repository Pattern** and native **Dependency Injection** mapped across a strict **Clean Layered Architecture** (Domain, Repository, Service, Handler).

Additionally, it implements advanced backend resilience patterns including a generic **Timeout Middleware Pattern** safeguarding against database stalling, a **Fast-Path Cache Read** pattern for instant idempotency deduplication, and a **Deadlock Immunity Strategy** that dynamically alphabetizes resource locks to serialize concurrent cross-transfers identically.

## AI disclosure

I utilized the **Antigravity** AI agent for development to accelerate boilerplate and generate rigorous integration tests. To ensure correctness before cleverness, I directed the agent using highly specific, senior-level architectural constraints. **Crucially, I maintained full human oversight: I reviewed every single change during the planning phase, audited all generated iterations during implementation, and manually enforced strict edge-case validation throughout the testing phase.**

1. **Architecture & Planning**: *"Act as a highly skilled Senior Golang Developer with deep architectural expertise. I am providing you the assignment.md file. Let's analyze the requirements and plan the core system architecture first, then split the development task into sensible, isolated commit chunks."*
2. **Schema & Persistence Setup**: *"Design the PostgreSQL schema using strict numeric bounds. Enforce `balance >= 0` natively on the wallets table. Implement a unique index constraint on the mapped idempotency key inside the transfers table to mathematically guarantee zero data duplication on aggressive concurrent network retries. Establish double-entry ledger records utilizing Foreign Keys linked to the transfer ID representing strict debit and credit sequences."*
3. **Repository & Transaction Strategy**: *"Code the PostgreSQL Repository pattern using the standard library `database/sql`. Utilize pessimistic row-level locking via `SELECT ... FOR UPDATE` exclusively bounded within an atomic transaction context. To explicitly nullify read-then-write double spends, read the balance *after* acquiring the row lock. To guarantee deadlock immunity during intersecting cross-transfers, dynamically sort the target wallet IDs alphabetically in memory before acquiring the query lock."*
4. **Domain & Validations**: *"Isolate all structural state rules into the Domain model structs. Create a robust validation method on the core struct that encapsulates invariant rules (e.g., negative amounts, empty idempotency keys, self-transfers) so they never leak into the Handler or Repository mapping layers."*
5. **Idempotency Strategy Orchestration**: *"Construct the Service Layer orchestrator managing the transfer workflow. Implement a robust dual-layer Idempotency Strategy: First, execute a fast-path database query on the idempotency key; if a network split occurs resulting in identical parallel executions, trap the inevitable unique constraint database exception natively within the transaction, and gracefully return the original identical cached payload safely."*
6. **Transport & Formatting**: *"Build a fundamentally thin HTTP Transport layer. The Handlers must exclusively decode JSON routing payloads to strong structs, trigger the Service layer interface natively, and marshal Go error variants directly into strongly-mapped HTTP status codes with zero embedded business execution logic."*
7. **Simulation & Concurrency Testing**: *"Develop rigorous test suites to mathematically stress test your lock topology. Create integration tests using Go wait-groups. Force 20 concurrent threads to aggressively withdraw from a wallet with only enough balance for 10 transfers, proving exactly 10 succeed and 10 fail to prevent double-spending. Then, force 5 threads to blindly retry identical idempotency keys at the exact same millisecond, proving exactly 1 succeeds and 4 are cleanly aborted. Ensure the tests evaluate pure behavior, not implementation details."*

## Schema Design

- `wallets`: Tracks native balances with an explicit `CHECK (balance >= 0)` constraint limiting negative bouncing.
- `transfers`: Maintains ledger state machine. Employs a `UNIQUE` index on `idempotency_key` natively blocking mathematically duplicated requests.
- `ledger_entries`: A dual-entry strictly enforcing two rows (DEBIT and CREDIT) per successful atomic `transfer_id`.

## Idempotency Strategy

Utilizes a multi-layered defense mechanism:
1. **Cache Read (Fast Path)**: Service explicitly intercepts any known `idempotency_key` and returns the historical identical payload instantly bypassing transactions.
2. **Database Constraint (Concurrency Defense)**: If two exact same duplicates hit precisely simultaneously, PostgreSQL evaluates the `UNIQUE` DB constraint. The database guarantees one fails aggressively (`23505 unique_violation`). The Go Service catches this exact failure, cleanly rolls back to protect state, and returns the successful sibling's data.

## Concurrency Strategy

- **Boundary**: Entirely handled within ACID bounds via `ReadCommitted`.
- **Locking**: Employs pessimistic row-level locking (**`SELECT ... FOR UPDATE`**) entirely eliminating the read-then-write double-spending race condition.
- **Deadlock Immunity**: Wallet targets are dynamically ordered alphabetically inside Go before row locks are requested. Simultaneous cross-transfers (A -> B and B -> A) queue perfectly instead of deadlocking the server.

## How to Run

1. `docker-compose up --build` for immediate isolated simulation.
2. Or purely native: `go run cmd/server/main.go` (Ensure `.env` provides the `DATABASE_URL`).

## How to Test

- `go test ./... -race -cover` to execute strict coverage.
- Uses massive-scale automated concurrency simulations (`transfer_integration_test.go`) firing 20 background `goroutines` simultaneously mathematically proving double-spend database lock stability.
- Features `transfer_benchmark_test.go` confirming Service isolation.

## Tradeoffs / Assumptions

- Using raw pessimistic DB locks trades tiny fractions of queue milliseconds vs relying on ultra-complex Kafka/Redis sagas, massively lowering operational monolithic footprint while guaranteeing absolute ACID validity.

## Optional Enhancements Completed

- **Wallet Balance API**: Added `GET /wallets/{id}` as a cleanly isolated module decoupled from the main transfer logic.
- **Transfer History API**: Added `GET /wallets/{id}/transfers` returning chronologically sorted `LIMIT 100` subsets using efficient B-Tree SQL indexing.
- **Observability & Logging**: Upgraded all generic server outputs into Go's native `log/slog` structured JSON telemetry system for production-grade observability.
- **Retry-Safe Workflows**: Mathematically baked into the framework. The dual-path Idempotency logic gracefully catches aggressive retries, rendering all network splits fundamentally retry-safe.

## Checklist

- [x] Tests pass
- [x] Lint passes
- [x] Format check passes
- [x] README or notes updated
- [x] PR description explains schema, idempotency, and concurrency

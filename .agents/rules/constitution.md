# Project Constitution

This file defines strict rules and behavioral constraints for the AI agent working on the Wallet Transfer Service project.

## Core Language & Libraries
1. **Go Standard Library Only**: The project is strictly built using the Go standard library. 
   - DO NOT use external frameworks (e.g., Gin, Fiber, Echo) for HTTP routing.
   - DO NOT use ORMs (e.g., GORM, SQLx) for database interactions. Use the standard `database/sql`.
   - The only acceptable third-party dependency is `github.com/lib/pq` for the PostgreSQL driver.

## Execution Context
1. **Working Directory**: All commands related to building, testing, or running the service must be executed from the `wallet-service` directory.
   - Example: `cd wallet-service && make test`
2. **Makefile**: Use the existing `Makefile` targets (`make build`, `make test`, `make run`, `make db-up`) for all common operations rather than typing out raw `go` commands.

## Architecture
1. **Layered Design**: Maintain the strict separation between Handler, Service, and Repository layers. Do not mix business logic into handlers or repositories.
2. **Idempotency**: All `POST` / write endpoints must strictly enforce idempotency and safe retries under concurrent load.

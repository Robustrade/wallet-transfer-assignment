# Concurrency Tests

Concurrency tests must run against real PostgreSQL and verify concurrent debits, same-key retries, and opposite-direction transfers. Use `go test -race ./...` for Go race detection in addition to database-level assertions.

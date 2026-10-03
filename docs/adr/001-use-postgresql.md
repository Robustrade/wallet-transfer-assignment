# ADR-001: Use PostgreSQL, with embedded Postgres in tests

- **Status:** Accepted
- **Date:** 2026-10-03
- **Design doc:** [§2 Technology Choices](../design.md#2-technology-choices)

## Context

The assignment prefers PostgreSQL and accepts SQLite. Concurrent debits on the
same wallet must be safe, and `go test ./...` must pass with no external
service running, because CI starts no database (requirements D-16).

## Decision

Use PostgreSQL 16 through pgx. Tests start their own Postgres with
`fergusstrange/embedded-postgres`, so the whole suite, including the
idempotency and concurrency tests, runs against a real database in CI.

## Consequences

- Row-level locks and constraints can enforce correctness in the database
  (ADR-006).
- Tests run against the same database as production; persistence is never
  faked.
- The first test run downloads Postgres binaries and needs network access.
- Tests fail if run as root, because Postgres refuses to start as root.
- Starting Postgres adds a few seconds to each test run.
- Running the service locally needs Docker Compose for Postgres.

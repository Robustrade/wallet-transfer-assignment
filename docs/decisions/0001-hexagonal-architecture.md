# ADR 0001: Hexagonal layout for a small service

## Status

Accepted

## Context

The assignment asks for a handler, a service, a repository, and domain models. The repository guidelines also ask for ports and adapters, with domain logic independent of PostgreSQL and HTTP.

## Decision

Use four rings:

- `internal/domain` for entities, transitions, and errors
- `internal/app` for use cases, depending only on `port` interfaces
- `internal/adapter/httpapi` and `internal/adapter/postgres` for delivery and persistence
- `cmd/wallet-service` as the composition root

Interfaces live next to the use cases that need them (`internal/app/port`), not next to the Postgres implementation.

## Consequences

- HTTP tests can run the real transfer use case against an in-memory port.
- Postgres tests can run the same use case against SQL without going through HTTP.
- There is no generic repository framework and no dependency-injection container. `main` calls constructors.

## Alternatives

A flat `handler/service/repository` tree would match the assignment example and would be slightly shorter. It would also make the database types visible to the use case unless an interface was introduced anyway. The port is the smaller difference that keeps the dependency rule, so the extra package is worth it. A framework such as Wire or Fx is not.

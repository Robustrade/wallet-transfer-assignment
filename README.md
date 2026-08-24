# Wallet Transfer Service

A wallet-to-wallet transfer service: idempotent `POST /transfers`, a double-entry ledger, wallet
balances, and concurrency-safe debits. Python, FastAPI, and PostgreSQL (`asyncpg`), built against
ten reviewed Architecture Decision Records.

## Design

Start with [`ARCHITECTURE.md`](./ARCHITECTURE.md) — an Executive Summary, a System Overview
diagram, and one section per component (`handlers`, `services`, `repositories`, `domain`). Every
non-trivial design decision (schema, idempotency mechanism, locking strategy) has its own ADR in
[`docs/decisions/adrs/`](./docs/decisions/adrs/), all Accepted:

| ADR | Decision |
|---|---|
| [0001](./docs/decisions/adrs/adr-0001-language-and-persistence-choice.md) | Python + PostgreSQL |
| [0002](./docs/decisions/adrs/adr-0002-idempotency-strategy.md) | Idempotency strategy |
| [0003](./docs/decisions/adrs/adr-0003-concurrency-locking-strategy.md) | Concurrency / locking strategy |
| [0004](./docs/decisions/adrs/adr-0004-schema-and-migrations.md) | Schema and migrations |
| [0005](./docs/decisions/adrs/adr-0005-api-contract-first-openapi.md) | API contract-first (OpenAPI) |
| [0006](./docs/decisions/adrs/adr-0006-interface-first-tdd.md) | Interface-first TDD |
| [0007](./docs/decisions/adrs/adr-0007-async-io-asyncpg.md) | Async I/O end-to-end via `asyncpg` |
| [0008](./docs/decisions/adrs/adr-0008-single-request-transfer-resolution.md) | Single-request transfer resolution |
| [0009](./docs/decisions/adrs/adr-0009-stored-balance-not-derived.md) | Stored balance, not ledger-derived |
| [0010](./docs/decisions/adrs/adr-0010-demo-wallet-seeding.md) | Demo wallet seeding |

[`HANDOVER.md`](./HANDOVER.md) is the implementation kickoff briefing, kept as a historical
record; [`AGENTS.md`](./AGENTS.md) is the canonical agent context (hard rules, layering contract,
forbidden patterns).

## Run it

```
just demo
```

Full containerized stack — Postgres, the app, and four seeded demo wallets. See
[`.agents/skills/run-app/SKILL.md`](./.agents/skills/run-app/SKILL.md) for the local-dev mode
(`just up` + `just dev`) and the concurrency demo (`just simulate`).

## Test it

```
just ci
```

Lint, format-check, the full test suite (domain, service-layer against an in-memory fake,
repository/end-to-end against real Postgres via `testcontainers`), and the OpenAPI drift check.

## API contract

[`openapi/spec.yaml`](./openapi/spec.yaml) is the hand-authored source of truth for the API;
Swagger UI is available at `/docs` once the app is running.

## AI usage

See this submission's PR description for the AI disclosure, and
[`docs/ai-transcripts/`](./docs/ai-transcripts/) for the full session transcripts.

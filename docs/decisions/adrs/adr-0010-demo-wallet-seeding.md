---
kind: adr
status: accepted
owner: Kartik Mittal
last_reviewed: 2026-08-24
---

# ADR-0010 — Demo wallet seeding: flag-gated, always-last Flyway migration

- **Status:** Accepted
- **Deciders:** Kartik Mittal (candidate)

## Context

`POST /transfers` is the only endpoint any ADR designs (ADR-0002 through ADR-0009) — found on
review to leave a real gap: nothing describes how a `wallets` row, with a known id and starting
balance, comes to exist at all. ADR-0006's test fixtures solve this for the automated suite only
(direct DB inserts per test, bypassing the API). ADR-0003's `scripts/simulate.py` ("seeds wallets
and drives the real running HTTP API") and manual `docker compose up` exploration have no
documented path to get a wallet into existence, since `wallets.id` is an opaque, application-
generated UUIDv7 (ADR-0004) with nothing to predict it from. `ASSIGNMENT.md` never asks for a
wallet-creation endpoint — it isn't in the required scope or even the Optional Enhancements list
(only a wallet *balance* read API and a transfer *history* read API are mentioned) — so building
one is scope this assignment doesn't call for.

## Decision drivers

- Must not silently insert demo data into every environment a migration runs against — a real
  deploy (or a grader's own environment) shouldn't get fake wallets it never asked for.
- Must stay inside the assignment's actual scope: no new endpoint, since none is asked for.
- Must be reliably ordered *after* every real schema migration, not just after the four that exist
  today — this has to keep holding as `V5`, `V6`, ... are added later.
- Reuses the migration mechanism ADR-0004 already established rather than inventing a second one.

## Considered options

1. **Unconditional seed migration alongside the schema migrations** (e.g. a `V5__seed_demo_wallets.sql`
   in `db/migrations/`, applied every time `just migrate` runs). Rejected: no opt-out — every
   environment, including CI and any real deploy, would get fake wallets whether it wanted them or
   not.
2. **Seed logic in application startup code** (e.g. "if `wallets` is empty, insert demo rows" run
   on service boot). Rejected: migrations are a deploy/setup-time concern, not part of the
   service's runtime path (ADR-0004) — this would violate that split, and makes seeding an
   implicit side effect of starting the process rather than a visible, opt-in action.
3. **A separate `db/seed/` Flyway location, only added to the migrate command when a
   `SEED_DEMO_DATA` flag is set, versioned in a reserved high band so it always sorts last.**
   *Chosen.*

## Decision

- Demo seed data lives in `db/seed/`, **not** `db/migrations/` — a separate Flyway `locations`
  path, kept out of the default migration run entirely.
- `just migrate` (ADR-0004) only ever applies `db/migrations/`. A `SEED_DEMO_DATA=true` flag
  (an environment variable, not a real feature-flag platform — proportionate to this project's
  scope) additionally includes `db/seed/` in Flyway's `-locations` argument for that run:
  ```
  just migrate                      # schema only — the safe default everywhere
  SEED_DEMO_DATA=true just migrate  # schema + demo wallets — local/demo use only
  ```
  Unset (or any value other than `true`), the seed location is never passed to Flyway, so a
  plain `just migrate` — including whatever CI and `testcontainers` invoke (ADR-0006) — never
  sees it. Demo data is opt-in by construction, not opt-out.
- **Version numbers in `db/seed/` are reserved to the `V9000`–`V9999` band** (e.g.
  `V9001__seed_demo_wallets.sql`), never used by `db/migrations/`. Flyway merges all configured
  locations into one globally version-ordered sequence, so this band is what actually guarantees
  the seed applies after every real schema migration, today and after any number of future ones —
  not the fact that it lives in a different folder, which by itself says nothing about ordering.
  This convention is documented here and must be respected by any future migration author; Flyway
  itself won't stop a schema migration from being numbered `V9002` by mistake.
- Seed content: a small fixed set of wallets (e.g. 3-4) with round starting balances and stable,
  hardcoded UUIDs — predictable enough for `scripts/simulate.py` (ADR-0003) and manual
  `docker compose` exploration to reference by id directly, without querying for what got created.
  Exact ids/amounts are an implementation detail decided when the migration is written, not fixed
  here.

## Consequences

**Good:** no environment gets demo data unless it explicitly asks for it via `SEED_DEMO_DATA=true`
— CI, `testcontainers`-backed tests, and any real deploy all get the safe default. `scripts/simulate.py`
and manual dev testing get known, addressable wallets without needing a wallet-creation endpoint
that's outside this assignment's scope. Ordering is guaranteed by a reserved version band, not by
directory placement alone, so it survives an arbitrary number of future schema migrations.

**Bad / risks:** the reserved `V9000+` band is a convention, not a mechanically enforced rule — a
future contributor could number a real schema migration into that range by mistake and silently
break the "seed is always last" guarantee; mitigated by documenting it here and keeping schema
migrations nowhere near that range in practice (four exist today). The `SEED_DEMO_DATA` env var is
a minimal, project-local gate, not a real feature-flag system — accepted as proportionate to a
3-5 hour assignment; a production system would likely want this decoupled from the migration
runner entirely (e.g. a separate seed script invoked explicitly, never via the same command as
schema migrations), which is a reasonable next step this ADR doesn't attempt to solve.

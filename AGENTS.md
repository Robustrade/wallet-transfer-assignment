> Canonical agent context (AGENTS.md open standard). `CLAUDE.md` is a symlink to this file.

# Wallet Transfer Service — Agent Context

A wallet-to-wallet transfer service: idempotent `POST /transfers`, double-entry ledger, wallet
balances, `PENDING/PROCESSED/FAILED` state machine, concurrency-safe debits. Take-home interview
assignment (`ASSIGNMENT.md`) — the graded rubric is `evaluation_guide.md` and
`.github/copilot-instructions.md`.

## Project structure

```
ASSIGNMENT.md                — the graded spec; evaluation_guide.md is the reviewer rubric
HANDOVER.md                  — implementation kickoff briefing (historical — see its own status note)
ARCHITECTURE.md              — component docs, kept in sync with src/wallet_transfer/
docs/decisions/adrs/         — MADR decision records; copy adr-000 for a new one
src/wallet_transfer/         — application source (handlers/, services/, repositories/, domain/)
openapi/spec.yaml            — hand-authored API contract; handlers/generated_models.py is built from it
tests/                       — pytest suite (domain/, service/, repository/, e2e/ tiers)
scripts/                     — check_openapi_drift.py (CI gate) and simulate.py (manual demo tool)
db/migrations/, db/seed/     — Flyway schema migrations and the flag-gated demo-wallet seed
.importlinter                — enforces handler -> service -> repository -> domain layering
.agents/skills/               — reusable Claude Code skills; .claude/skills/ symlinks here
docker-compose.yml, Dockerfile — Postgres + the app container itself (`just demo`/`just up`)
```

## Hard rules

- Monetary amounts are integer minor units (cents) or `Decimal` — never `float`.
- Idempotency: the idempotency record and the transfer/ledger writes happen in the **same**
  database transaction. Checking for an existing key and writing the new one must not be two
  separate transactions — that reopens the race idempotency exists to close.
- Every transfer writes exactly two ledger entries (one DEBIT, one CREDIT) in the same
  transaction as the balance/state update. Never split across transactions or add a ledger entry
  without an owning transfer.
- Concurrency strategy for same-wallet debits: ADR-0003 — lock wallets in ascending `wallet_id`
  order, *before* writing anything that references them (not after — see ADR-0002/0003's
  wallet-locking-order note), to avoid deadlocks between two transfers that touch the same pair
  of wallets in opposite directions.
- Handlers: request validation + transport mapping only. No business logic, no direct
  repository access.
- Repositories: persistence only. No workflow decisions (e.g. no "if insufficient funds" branch
  in a repository).
- Domain models own state-transition validation — `PENDING -> PROCESSED` or `PENDING -> FAILED`
  only, never an invalid jump, and never leave a transfer stuck if a step fails partway.
- Non-trivial design decisions (schema, idempotency mechanism, locking strategy) get an ADR in
  `docs/decisions/adrs/` **before** implementation, per `ASSIGNMENT.md`'s own
  Documentation-First Workflow. Use `/new-adr` or copy `adr-000-madr-template.md` directly.
- Tests are behavioral: assert on responses/DB state, not internals. A feature change ships
  with its test in the same PR — Red (failing test) -> Blue (smallest correct fix) -> Green
  (refactor with tests still passing), per `ASSIGNMENT.md`.
- Interface-first, per ADR-0006: define a layer's contract (domain entity shape, repository
  `Protocol`, service method signature, or the OpenAPI spec for handlers) before implementing it,
  then TDD against that contract. Service-layer tests run against an in-memory fake repository
  (fast, many cycles); repository-layer and end-to-end tests run against real Postgres via
  `testcontainers` (slower, fewer, where real locking/constraint behavior actually gets proven).

## How to work here

- `just ci` before every push — lint, format-check, the full test tier (unit/domain, service
  against the in-memory fake, repository/e2e against real Postgres via `testcontainers`), and the
  OpenAPI drift check.
- `just fmt` to auto-format.
- `just install` after changing `requirements.txt` or `requirements-dev.txt`.
- `just pre-commit-install` once, after cloning.
- Conventional commits (`feat:`, `fix:`, `docs:`, `chore:`, `refactor:`, `test:`) — commit
  message quality is explicitly graded (`evaluation_guide.md` "Development practices").
- `just up` for a local Postgres, or `just demo` for the full containerized stack (Postgres + app,
  with demo wallets seeded) — see the `/run-app` skill. Copy `.env.example` to `.env`.

## Architecture documentation

Every top-level module directly under `src/wallet_transfer/` needs a matching `## <Name>`
section in `ARCHITECTURE.md`: purpose, public interface, dependencies, and a small ASCII diagram
if the internal flow isn't obvious from the name. Add it in the **same PR** that adds the
component — don't defer it.

## Skills

- `/new-adr` — scaffold a new ADR from the MADR template with the next sequential number.
- `/assignment-review` — self-review the diff against `evaluation_guide.md` and
  `.github/copilot-instructions.md`'s rubric before opening the PR.
- `/run-app` — launch the service (containerized demo stack or local `.venv` + uvicorn), apply
  migrations with or without demo seed data, verify it's serving requests, and drive
  `scripts/simulate.py`'s concurrency demo.

## AI disclosure

`ASSIGNMENT.md`'s "AI usage" section and the PR template both require: which tool was used, how
it's generally used, and a transcript of the full AI session (or every prompt, if a transcript
isn't possible). Don't leave this until the end — export/compile it before opening the PR.

## Forbidden patterns — never do these

- `git commit --no-verify` — fix the violation, never skip the gate.
- Float for any monetary value.
- Hardcode secrets — use `.env` (gitignored) and `.env.example` as the template.
- Checking idempotency in a separate transaction from the write it's supposed to guard.
- A schema, idempotency, or locking decision with no corresponding ADR.

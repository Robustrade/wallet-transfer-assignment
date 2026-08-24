---
kind: adr
status: accepted
owner: Kartik Mittal
last_reviewed: 2026-08-24
---

# ADR-0001 — Implement the wallet transfer service in Python against PostgreSQL

- **Status:** Accepted
- **Deciders:** Kartik Mittal (candidate)

## Context

`ASSIGNMENT.md` does not mandate a language or datastore: it names PostgreSQL as "preferred" and
SQLite as an "acceptable alternative," and says nothing about runtime. The repo's pre-existing
`.github/workflows/ci.yml`, however, is already wired for Go (`actions/setup-go`, `golangci-lint`,
`gcc` + `CGO_ENABLED`), which is a signal worth weighing, not a hard requirement — nothing in the
candidate-facing docs states it as one, and the CI is otherwise designed to be language-agnostic
via repository variables (`LINT_CMD`, `FORMAT_CHECK_CMD`, `TEST_CMD`).

## Decision drivers

- Evaluation is on schema design, transaction/locking strategy, idempotency correctness, and
  layering — not on matching an unstated language preference.
- The chosen datastore needs to demonstrate a real concurrency story (row-level locking), which
  favors a database with mature, well-understood isolation semantics over one chosen for
  zero-infra convenience.
- Time-box is 3-5 hours; the toolchain should not spend that budget fighting unfamiliar plumbing.

## Considered options

1. **Go + PostgreSQL.** Matches the CI's existing setup steps out of the box. Rejected as the
   default only because it isn't a stated requirement, and switching away from it is a deliberate,
   disclosed choice rather than an oversight — this ADR exists specifically to make that explicit.
2. **Python + SQLite.** Zero local infra, and `CGO_ENABLED` in the CI env hints the template
   authors may have expected a cgo-based SQLite driver. Rejected: SQLite's coarser
   database-level locking makes for a weaker demonstration of the row-level concurrency control
   the assignment explicitly asks to be justified.
3. **Python + PostgreSQL.** *Chosen.* Postgres gives `SELECT ... FOR UPDATE` / row-level locking
   and real transaction isolation levels to reason about and justify, which is exactly what
   Section 5 (Concurrency Safety) and the evaluation guide's "Transaction and Locking Strategy"
   section are grading.

## Decision

Implement the service in Python, persisting to PostgreSQL. Local/dev Postgres runs via
`docker-compose.yml`; automated tests spin up their own throwaway Postgres via
`testcontainers-python` rather than depending on any CI-specific service-container feature, so the
same test suite runs identically locally and in whatever CI environment executes it.

## Consequences

**Good:** Postgres's row-level locking and explicit isolation levels give a concrete, justifiable
answer to the concurrency requirement. Python keeps the layered architecture (handler / service /
repository / domain) straightforward to express and to enforce mechanically (see the
`.importlinter` contract).

**Bad / risks:** `.github/workflows/ci.yml`'s language-specific steps (Go setup, `golangci-lint`
install, `gcc`) no longer apply and needed reworking for Python — done in this same change; the
var-driven `LINT_CMD`/`FORMAT_CHECK_CMD`/`TEST_CMD` structure itself was already language-agnostic
and is left intact. `runs-on: [actions_runner_dev_new]` is deliberately left untouched: this fork
has no self-hosted runners registered, but the grading PR targets the upstream `Robustrade` repo,
where that label is presumably a real runner — changing it here could silently break the actual
grading run in an environment this candidate can't observe or verify.

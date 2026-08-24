---
kind: adr
status: accepted
owner: Kartik Mittal
last_reviewed: 2026-08-24
---

# ADR-0005 — API contract-first development: hand-authored OpenAPI spec, generated models, Swagger UI

- **Status:** Accepted
- **Deciders:** Kartik Mittal (candidate)

## Context

FastAPI (already selected for the handler layer — async routes backed by `asyncpg`, per ADR-0007;
pydantic validation) defaults to the *opposite* of spec-first: it introspects route decorators and
Pydantic models at runtime to
generate an OpenAPI document, and ships Swagger UI (`/docs`) and ReDoc (`/redoc`) for free against
that auto-derived spec. `ASSIGNMENT.md` doesn't mention OpenAPI at all — this ADR exists because
of an explicit steer during design review: API development should follow OpenAPI-spec-based code
generation, with Swagger wired in, treating the API contract as a first-class, reviewable,
versioned artifact — consistent with the review discipline already applied to schema, idempotency,
and concurrency in ADR-0001 through ADR-0004.

## Decision drivers

- The contract should be authored and reviewed like any other design artifact, not be a byproduct
  the code happens to produce — matches this project's Documentation-First workflow.
- Must not fight the required handler/service/repository/domain layering — a fully generated
  server stub would blur or bypass it.
- Interactive docs (Swagger UI) should work with no custom wiring effort.
- Stay inside the assignment's time-box — favor mature, purpose-built tools over hand-rolling a
  spec-to-code pipeline.

## Considered options

1. **Pure code-first (FastAPI's default).** Routes and Pydantic models in code; OpenAPI JSON
   auto-derived at runtime; Swagger UI free at `/docs`. Rejected as the *sole* approach: the
   contract is then a byproduct of the implementation, not an authored-first artifact — doesn't
   meet the explicit ask for spec-based codegen.
2. **Full spec-first with generated server route stubs** (e.g. `openapi-generator`'s
   `python-fastapi` target, or `fastapi-code-generator`'s route generation, not just its model
   generation). Rejected: generated server stubs fight the layering this project is graded on,
   and regenerating them risks clobbering hand-written business logic unless awkwardly split into
   thin generated wrappers calling hand-written implementations. This pattern is far more idiomatic
   in Java/Go OpenAPI-generator ecosystems than in Python's — more ceremony than the time-box
   justifies here.
3. **Spec-first for the contract, generated models only, hand-written FastAPI routes, drift-checked
   against the code's own auto-derived OpenAPI JSON.** *Chosen.*

## Decision

- `openapi/spec.yaml` is authored **by hand** as the versioned source of truth for the API
  contract — reviewed and changed in the same PR as the behavior it describes, same discipline as
  the ADRs and Flyway migrations (ADR-0004). For this assignment's scope that's realistically the
  `POST /transfers` contract plus the optional read endpoints if built.
- Request/response Pydantic models are **generated from** `openapi/spec.yaml` via
  `datamodel-code-generator` into a dedicated, never-hand-edited module (exact path decided during
  implementation, e.g. `src/wallet_transfer/handlers/generated_models.py`) — regenerated via
  `just generate-models` whenever the spec changes, not committed-then-drifted.
- Route handlers themselves are **hand-written** FastAPI routes using those generated models for
  typing — keeps the handler layer thin and inside the layered architecture / `.importlinter`
  contract, rather than being replaced by generated server code.
- Swagger UI / ReDoc: FastAPI's built-in `/docs` and `/redoc` are used as-is, backed by its own
  runtime-introspected OpenAPI JSON — free, zero custom wiring required.
- **Drift check** (CI gate, blocking): generate the app's live OpenAPI document (via
  `app.openapi()`, no running server needed) and diff its paths/schemas against
  `openapi/spec.yaml`; fail CI on divergence. This is what actually keeps the hand-authored
  contract and the real implementation honest with each other — without it, two independently
  maintained descriptions of the same API will drift silently, which is the real risk a spec-first
  approach introduces if nothing enforces the link back to the code.

## Consequences

**Good:** the API contract is a first-class, reviewable artifact from the start, consistent with
every other design decision in this project. Swagger UI is available with no custom wiring.
Generated Pydantic models remove a class of hand-transcription bugs between spec and code. The
drift check prevents silent divergence between the two — the actual failure mode most spec-first
setups don't guard against.

**Bad / risks:** two artifacts describe request/response shapes on their face
(`openapi/spec.yaml` and the generated models) — mitigated by treating the models as a pure build
artifact (never hand-edited, regenerated on every spec change) and by the drift check catching any
place implementation and contract disagree. Adds `datamodel-code-generator` as a dev dependency
and a `just generate-models` step to the workflow. A generated client SDK for `scripts/simulate.py`
(ADR-0003's demo tooling) is a natural extension of this same spec but is left optional, not
committed, to stay inside the time-box.

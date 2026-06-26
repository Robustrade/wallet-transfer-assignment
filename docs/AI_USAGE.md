# AI Usage Disclosure

This section discloses, honestly and in detail, how AI assistance was used to
produce this submission. It maps to the three points required by the PR
template.

## 1. Tool used

**Claude Code** (Anthropic's agentic CLI), driving Claude (Opus-class) models.
It was the only AI tool used for this submission.

## 2. How the tool was generally used

The work followed a **documentation-first, contract-driven** approach rather
than ad-hoc generation:

1. **Design before code.** The problem, API contract, data model, idempotency
   mechanism, and concurrency strategy were written down first (this is captured
   in `docs/DESIGN.md`). The hard decisions — pessimistic `FOR UPDATE` with
   sorted lock ordering, idempotency enforced by a database unique constraint,
   durable `FAILED` transfers — were settled at design time, not improvised
   during implementation.

2. **A shared foundation contract.** The domain layer (entities, the `int64`
   money type, the transfer state machine, sentinel errors) and the service
   ports were established first as the stable contract every other layer codes
   against. This kept the layered, ports-and-adapters architecture honest:
   dependencies point inward and the service owns its interfaces.

3. **Parallel implementation, single owner for the core.** With the contract
   fixed, independent layers — the Postgres repository adapters, the HTTP
   handler, and the documentation — were developed in parallel (fanned out to
   focused sub-tasks), while the orchestrator retained ownership of the pieces
   that must stay coherent: the domain foundation, the service orchestration,
   the integration wiring in `cmd/walletd`, and the end-to-end concurrency
   tests. The riskiest, most correctness-sensitive code was kept under direct
   review rather than delegated.

4. **TDD discipline.** Behavior was pinned with tests: pure unit tests for the
   domain and service (against in-memory fakes), handler tests for HTTP mapping
   and strict JSON, and integration tests that run the real pgx adapters against
   a throwaway `postgres:16-alpine` via testcontainers — including concurrency
   tests that assert exactly-once and no-double-spend against a real database.

5. **Human review and verification of everything generated.** All AI-produced
   code, configuration, lint rules, and documentation were read, reviewed, and
   verified — tests run with the race detector, `golangci-lint` and `gofmt`
   gates applied, and behavior checked against the design — before being
   accepted. AI was used to accelerate and stay disciplined, not to bypass
   judgement; the design decisions and their tradeoffs are owned by the author.

## 3. Transcript / prompts

The full session transcript will be provided with the submission (attached to
the repository or emailed alongside the PR, per the template's options). In
addition, this document together with the **commit history** captures the
workflow and the sequence of intent that produced the solution. If the transcript
cannot be attached for any reason, the prompts used will be supplied directly on
request.

# AI Usage Disclosure

Kulu asks for an honest account of AI usage on this submission. Everything below is drawn from the actual session record of this assignment (24-25 September 2026). Nothing is inferred or embellished.

## 1. AI tool used

**Qoder** — an agentic coding assistant with direct file-system and terminal access to the repository, used in my normal development environment (Docker Desktop, PostgreSQL 17). All the AI-assisted work described here ran through this tool, under a staged workflow I defined.

## 2. How I generally used AI

I did not hand it an open-ended "build this" prompt. I ran a 17-stage gated workflow of my own design: repository inspection → requirement analysis → architecture options → schema design → transaction design → hostile self-review → implementation plan → implementation → idempotency review → concurrency tests → ledger integrity review → failure tests → code-quality review → performance review → README → this disclosure. Each stage produced one artifact, which I reviewed before issuing the next stage. Constraints I set and enforced:

- No code changes in review stages; no implementation until the design was approved.
- No unnecessary abstractions, no new dependencies unless necessary, no unrelated changes.
- Stay inside the assignment's 3-5 hour scope; no optional enhancements.
- Don't claim anything that isn't actually implemented or verified.

## 3. What AI helped with

- Inspecting the repository, analyzing requirements, and surfacing ambiguities and assumptions for my approval.
- Design work: balance strategy, concurrency control, idempotency storage, and ledger representation — presented as options with trade-offs — plus the PostgreSQL schema with its DB-enforced invariants.
- Designing the `POST /transfers` transaction: claim-first idempotency, ordered row locks, bounded waits, explicit commit/rollback behavior.
- Writing the full implementation: migrations, domain/service/store/HTTP layers, and 38 test functions (smoke, concurrency, failure, store, unit) run against real PostgreSQL, not mocks.
- Review stages I commissioned: hostile design review; idempotency review (3 findings); ledger integrity review (13 probes + whole-DB checks); staff-level code review (3 MEDIUM / 6 LOW findings); performance review.
- The README / PR description.
- A load test I requested after the implementation was complete: a throwaway load generator (kept outside the repo), nine workload configurations, and tail-latency analysis. Results: 1,557 tx/s across 50 wallet pairs (p99 52 ms), 3,687 req/s on idempotent replays (p99 22 ms), ~19,000 req/s on wallet reads, 340-470 tx/s on a single hot pair; after 119,580 transfers every balance and ledger invariant held exactly.
- A follow-up tuning pass I requested ("does using PgBouncer optimize it further; also fine tune the db connections and apply after verification"): a sweep of connection-pool sizes (8/16/32/64 per instance) under identical read, replay, and transfer workloads against real PostgreSQL, with the change applied only after that evidence. Findings: read and replay throughput plateaus at 32 connections; single-pair transfers are serialized by the wallet row lock and are pool-insensitive; every overload 503 traced to the 5-second statement timeout in the lock queue, none to pool exhaustion. Changes: pool default 16 → 32, the transaction path's 5-second acquisition bound extended to all pooled read paths, and a measured pool-sizing section plus an explicit PgBouncer assessment in the README. The final build then confirmed the gains under the same workload.

## 4. What decisions were made by me

- Defined the staged workflow and every gate, and directed what each review stage should target.
- Approved architecture, schema, transaction design, implementation plan, and implementation before each next stage proceeded.
- Authorized every fix set before it was applied: the idempotency fixes (commit-in-doubt → 503, write-once guard, NUL input rejection) and the code-quality fixes (database outage → 503, bounded pool acquisition, statement timeout, input validation).
- Required the same-key/different-payload case to be an explicit 409, never a silent first-result replay.
- Decided to keep the bounded pool-acquire behavior as deliberate load shedding, and that the concurrency tests were never to be weakened to make a failure pass.
- Directed that load-test tooling stay outside the repository so the deliverable remains clean.
- Required the connection-pool tuning to be applied only after verification under load, and directed that this optimization work be recorded in this disclosure.

## 5. What code was reviewed/verified by me

- No code was changed without my approval. Each stage's deliverable — files changed, tests executed, risks identified — was reviewed by me before approval; review-only stages produced zero edits.
- I reviewed and approved the specific behavior changes in the fix sets above, with their accompanying tests (error-mapping table, write-once idempotency guard, timeout-to-HTTP mappings, input validation).
- In this session I did not hand-write code or line-by-line audit every file; the deep verification was performed by the AI review stages, whose evidence and conclusions I reviewed at each gate.

## 6. Testing and validation performed by me

- I set the testing standard: integration tests against real PostgreSQL rather than mocks, explicit concurrency and failure scenarios, and an independent load test after implementation.
- I reviewed and accepted the test evidence at each gate: green `go test ./...` runs (including repeat runs to check for flakes), the concurrency races, the fault-injection results, the full load-test report, and the pool-tuning evidence (the connection sweep that justified the change and the confirmation run of the final build).
- I did not personally execute the test suites in this session — that was done by the AI; my validation was reviewing the results and approving, or directing fixes, at each gate.

## 7. Limitations of AI-generated suggestions

- First-pass work was not flawless and needed the review gates: for example, commit-in-doubt initially returned HTTP 500 (fixed to 503), there was no statement timeout initially, and the first README draft overclaimed for one 503 class before correction.
- AI's static performance estimate (~200 tx/s per hot wallet) was about 2x pessimistic versus the measured 340-470 tx/s; analytic claims needed empirical testing to correct.
- An AI assumption about database timeout semantics — that `lock_timeout` bounds a lock statement's total wait — was contradicted by the load evidence; a controlled experiment resolved it (the budget applies once per lock-acquisition attempt), the documentation was corrected, and the analysis confirmed the effective per-request bound is the 5-second statement timeout.
- Some AI analysis had to be redone when its own evidence collection was flawed (an early log capture produced empty evidence and a misleading "all clear"; a metric contradiction needed re-analysis before it was trusted).
- A rare p99 tail under extreme single-pair contention could not be fully root-caused; the environment-level conclusion is by exclusion, not direct observation, and is stated as such in the report.
- All measurements are single-host (Docker Desktop on macOS): no production-scale data and no real network conditions.

*This disclosure was drafted by the AI from the actual session record, at my direction; I reviewed and approved it.*

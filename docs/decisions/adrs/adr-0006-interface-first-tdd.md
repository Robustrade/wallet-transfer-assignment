---
kind: adr
status: accepted
owner: Kartik Mittal
last_reviewed: 2026-08-24
---

# ADR-0006 — Interface-first design driven by two-tier TDD

- **Status:** Accepted
- **Deciders:** Kartik Mittal (candidate)

## Context

`ASSIGNMENT.md` requires Red-Blue-Green discipline (failing test -> smallest correct solution ->
safe refactor), not tests backfilled after the fact, and the evaluation guide grades testing
discipline directly. This project is already contract-first at its edges — the schema is
versioned via Flyway migrations (ADR-0004) and the HTTP API is spec-first via a hand-authored
OpenAPI document (ADR-0005). Per explicit direction, that same discipline should extend inward:
once each layer's *interface* (its wireframe) is defined, TDD drives that layer's implementation,
not the other way around.

## Decision drivers

- `ASSIGNMENT.md`'s Red-Blue-Green requirement is explicit, not optional.
- Tests must be behavioral, exercised through a layer's own public interface — not
  implementation-detail assertions (evaluation guide, explicitly).
- TDD only survives a real time-box if the feedback loop is fast; a repository-backed test that
  spins up a fresh Postgres container is too slow to drive the many small cycles the service
  layer's idempotency and concurrency logic (ADR-0002, ADR-0003) needs.
- The interface boundary should reinforce the layering already enforced mechanically by
  `.importlinter`, not just restate it in prose.

## Considered options

1. **Implement everything first, add tests afterward.** Rejected outright — contradicts
   `ASSIGNMENT.md`'s explicit Red-Blue-Green requirement and the graded testing-discipline
   criterion.
2. **TDD against a real Postgres for every layer, including service-layer orchestration.**
   Rejected as the default for the service layer: correct, but every small orchestration-logic
   cycle would pay a testcontainer startup/teardown cost, which in practice discourages the many
   small Red-Blue-Green cycles TDD depends on. (Still the right choice for the repository layer
   itself — see Decision.)
3. **Define each layer's public contract as a `typing.Protocol` (or plain entity shape) before
   implementing it; drive implementation with failing tests against that contract; use an
   in-memory fake implementing the same repository Protocols to TDD the service layer without a
   database.** *Chosen.*

## Decision

Before implementing a layer, define its contract first:

- **Domain** — entity shapes (`Wallet`, `Transfer`, `LedgerEntry`) and their state-transition
  methods (e.g. `Transfer.mark_processed()`, `Transfer.mark_failed(reason)`), validation rules
  living on the entity itself.
- **Repository** — an async `typing.Protocol` per repository (`WalletRepository`,
  `TransferRepository`, `LedgerRepository`, `IdempotencyRepository`; e.g. `async def
  get_for_update(...) -> Wallet`), per ADR-0007, declaring persistence operations with no
  implementation behind them yet.
- **Service** — orchestration method signatures (e.g.
  `TransferService.create_transfer(request) -> Transfer`) written against the repository
  Protocols, not concrete repository classes — dependency inversion, not layering by convention
  alone.
- **Handler** — the OpenAPI spec + generated models from ADR-0005 **are** this layer's wireframe;
  no separate contract step needed here.

Per slice, strictly: (1) define or extend the relevant Protocol/entity shape, (2) **Red** — a
failing test against that contract, (3) **Blue** — the smallest code that passes it, (4)
**Green** — refactor with tests staying green.

**Two DB-facing test tiers, not one:**
- **Repository-layer tests** run against a real Postgres via `testcontainers` — this is where
  locking and constraint behavior is real and must be proven real; a fake cannot demonstrate that
  `SELECT ... FOR UPDATE` ordering (ADR-0003) actually prevents a deadlock.
- **Service-layer tests** run against an in-memory fake implementing the same repository
  Protocols — fast enough to sustain the many Red-Blue-Green cycles the idempotency (ADR-0002) and
  concurrency (ADR-0003) orchestration logic needs, and able to deterministically construct races
  (a losing concurrent request, a duplicate key) that are awkward to force reliably against real
  DB timing.
- A smaller set of **end-to-end tests** (real Postgres, real HTTP layer via the FastAPI test
  client) close the loop and catch anything the fake didn't faithfully model.

**Fake vs. mock — not the same thing.** The service-layer test double is a *fake*: a second,
real (if simplified) implementation of the same repository `Protocol`, run entirely in memory. It
is never a *mock* — nothing in this project's tests patches internals, stubs a method to return a
canned value irrespective of input, or asserts on call counts/arguments. The evaluation guide
explicitly flags tests that assert implementation detail instead of behavior; a mock-heavy test
suite is the classic way that happens by accident, which is exactly what the fake avoids. For the
**repository and end-to-end tiers specifically, there is no mocking of the database at all** —
these run against a real Postgres via `testcontainers` and the real `asyncpg` pool the app itself
uses (ADR-0007), full stop. This is the tier that actually proves locking (ADR-0003) and
constraints (ADR-0004) work, and a mocked DB cannot prove that.

### Fixture design (repository / integration / end-to-end tiers)

- **Container lifecycle:** one Postgres `testcontainers` instance, scoped to the test *session*
  (not per-test) — starting a fresh container per test is too slow to sustain the CT1-CT7
  concurrency scenarios (ADR-0003), which each already run multiple concurrent operations.
  Flyway migrations (ADR-0004) are applied once, immediately after the container is ready.
- **Per-test isolation:** a transaction-rollback-per-test pattern (the common fast-isolation
  trick) does **not** work here — the repository/service layer legitimately issues real
  `COMMIT`s as part of what's being tested (locking and idempotency both depend on real commit
  visibility across separate connections), so nesting the test itself in an outer transaction
  would either be rolled back underneath the code under test or mask exactly the behavior being
  verified. Instead: a function-scoped fixture `TRUNCATE`s `wallets`, `transfers`,
  `ledger_entries`, `idempotency_records` (`CASCADE`) before each test, then seeds the wallets
  that specific test needs. Deterministic, no cross-test ID collisions, no reliance on rollback
  semantics the code under test would otherwise fight.
- **Concurrency scenarios need real concurrent connections**, not a single connection reused
  serially — CT1/CT2/CT6 (ADR-0003) drive their concurrent attempts via `asyncio.gather` over
  multiple coroutines, each pulling its own connection from the shared `asyncpg` pool, so the
  operations are genuinely in flight at once, not just sequential calls that happen to be
  unawaited. CT7 needs the same, plus one explicit synchronization point (not `asyncio.gather`
  alone) to land the injected retry inside the original request's inter-attempt gap
  deterministically, rather than relying on timing luck.
- **Parameterized tests** (`pytest.mark.parametrize`) cover scenario *families* rather than one
  hard-coded case per test: CT1's fan-in count (e.g. `[2, 5, 20]` concurrent attempts), CT4/CT5's
  idempotency payload variants (identical payload vs. a differing field per case), and the
  pure-validation scenarios from ADR-0002 (malformed body, self-transfer, non-positive amount) as
  one parametrized "rejected before any transaction" test. Non-existent wallet is deliberately
  **not** folded into that same group (inconsistency fixed on review) — it takes a different path
  (transaction opens, the idempotency record is inserted, the lock lookup reports "not found,"
  everything rolls back) and gets its own case asserting the rollback actually happened: no
  transfer row, no idempotency record, no ledger entries persist after the `404`.
- **CI enforcement:** `TEST_CMD` (`just test`) must run this full tier, not just the fast
  fake-backed service tests — CI is only a meaningful gate if it actually exercises the real
  Postgres/`asyncpg` path, not just the parts that happen to be fast. This requires the CI runner
  to have Docker socket access for `testcontainers` to start a container at all; see ADR-0007's
  Consequences — this Docker-access risk is separate from, and additional to, the runner-identity
  risk ADR-0001 flagged, not something ADR-0001 already covered.

### Required test scenarios (domain, service/fake, ledger, end-to-end)

The critical-section concurrency scenarios (CT1-CT7) belong to ADR-0003, since they're specific
to its locking mechanism. The rest of the required matrix belongs here:

**Domain tier (pure, no I/O):**
- **DT1** — `mark_processed()` / `mark_failed()` called on an already-terminal transfer is
  rejected — an illegal transition is unrepresentable, not just untested.
- **DT2** — only `PENDING -> PROCESSED` and `PENDING -> FAILED` are constructible; any other
  transition is rejected.

**Service tier (in-memory fake — deterministically simulating race *outcomes*, not real
concurrency; real concurrency is CT1-CT7's job):**
- **ST1** — fake reports "idempotency key already recorded, same fingerprint" → service returns
  the cached terminal result without calling any wallet-locking logic.
- **ST2** — fake reports "idempotency key already recorded, different fingerprint" → service
  returns `409`.
- **ST3** — fake raises a simulated retryable error (`lock_timeout`, `deadlock_detected`, or a
  connection-pool-acquire timeout — parametrized over all three, ADR-0003) on attempt 1, succeeds
  on attempt 2 → service retries and succeeds within the bounded attempt count.
- **ST4** — fake raises the same retryable error on every attempt → service exhausts retries and
  surfaces a distinct `503`, not a generic error (ADR-0003) — regardless of which of the three
  error classes triggered it.
- **ST5** — insufficient funds → service resolves the transfer to `FAILED`, writes zero ledger
  entries, does not retry.
- **ST6** — self-transfer / non-positive amount → rejected before any repository call, no
  idempotency record attempted (ADR-0002's validation boundary).
- **ST7** — non-existent `fromWalletId`/`toWalletId` (inconsistency fixed on review — this was
  previously folded into ST6, which wrongly claimed no repository call happens for this case
  too): the fake's idempotency-record insert succeeds first, since this is a well-formed request;
  the fake's wallet-lock call then reports "not found," and the service rolls back everything
  from this attempt — including the fake's idempotency record — and returns `404`. Proves the
  rollback is real, not a persisted `FAILED` transfer (ADR-0002's validation boundary, ADR-0003).

**Ledger-correctness tier (repository/integration + end-to-end):**
- **LT1** — after any `PROCESSED` transfer, exactly 2 ledger entries exist, one `DEBIT` one
  `CREDIT`, equal amounts.
- **LT2** — after any `FAILED` transfer, zero ledger entries exist.
- **LT3** — global invariant: `sum(CREDIT amounts) == sum(DEBIT amounts)` across the entire
  ledger, checked after a batch of concurrency scenarios, not just a single transfer.
- **LT4** — per-wallet reconciliation invariant (ADR-0009): `wallet.balance == initial_balance +
  sum(CREDIT) - sum(DEBIT)` for that wallet, checked specifically after the CT1/CT6 concurrency
  stress scenarios — this is the test that actually proves those scenarios didn't quietly
  corrupt anything.

**End-to-end tier:**
- **E1** — full HTTP round-trip (real FastAPI test client, real Postgres) for: a normal transfer,
  a sequential idempotent replay, and each validation-boundary rejection.
- **E2** — the OpenAPI drift check itself (ADR-0005) is a required, explicit CI check, not a
  pytest case — listed here so it isn't lost between ADRs.

## Consequences

**Good:** TDD stays fast enough to actually sustain through the time-box, since only the
repository and end-to-end tiers pay the real-Postgres cost. Defining Protocols first makes the
required layering literal in the type system, reinforcing rather than duplicating
`.importlinter`. Concurrency races become deterministic, fast unit tests at the service layer
instead of only slow, occasionally-flaky real-thread/real-DB tests.

**Bad / risks:** an in-memory fake can drift from the real repository's actual behavior (e.g. a
constraint violation it doesn't simulate) — mitigated by keeping the fake deliberately simple
(an in-memory store plus the same uniqueness/FK checks the schema itself enforces) and by the
end-to-end tier existing specifically to catch what the fake couldn't. Slightly more upfront
design work — defining contracts before any concrete implementation — accepted as the direct cost
of "wireframe first," which is the explicit ask this ADR formalizes.

# Implementation Handover — Wallet Transfer Service

> **Status: implementation complete.** This document is kept as the historical kickoff record —
> the constraints in §2 and the schema in §3 are still accurate references, but the framing below
> ("no code exists yet", ADR statuses, unchecked §7 boxes) describes the state *before*
> implementation started, not the current one. `ARCHITECTURE.md` is the up-to-date component
> reference; the ADRs themselves (now Accepted) are the source of truth for design decisions.

This hands the design off to whichever agent(s) implement `src/wallet_transfer/`. All design work
is done: ten ADRs, internally consistent, reviewed adversarially and fixed. Your job is to
implement, test, and verify against what's already decided — not to re-derive or re-litigate it.
If something here conflicts with an ADR, the ADR is the source of truth; if two ADRs conflict with
each other, that's a bug in this handover or a regression — stop and flag it, don't silently pick
one.

## 1. Required reading, in order

1. `ASSIGNMENT.md` — the graded spec. Ground truth for what "done" means.
2. `evaluation_guide.md` and `.github/copilot-instructions.md` — what gets checked, and what an
   automated reviewer is primed to flag.
3. `AGENTS.md` — hard rules, layering contract, forbidden patterns. Read this before writing a
   single line of code; it is binding, not advisory.
4. `ARCHITECTURE.md` — current state (now filled in — Executive Summary, System Overview, a `##`
   section per module) and Known Limitations (the deliberate scope cuts: no worker/queue, single
   currency, minimal observability, per-wallet serialization ceiling). Don't try to "fix" any of
   these — they're intentional.
5. `docs/decisions/adrs/adr-0001-*.md` through `adr-0010-*.md`, **in numeric order**. All ten are
   now Accepted — each was flipped only once its own governing tests in §6 were green, not as a
   bulk change.

## 2. Non-negotiable correctness constraints

These are fixes to real contradictions found during design review — each one broke a first-principles
implementation attempt before it was corrected. If you arrive at any of these from first principles
without having read the ADR, you will very plausibly get it wrong. Read the constraint, not just the
summary below — the "why" matters for judgment calls the summary can't cover.

- **Every table's primary key is generated application-side, in Python, as UUIDv7** — via the
  `uuid6` package's `uuid7()` (project targets Python 3.12; stdlib `uuid.uuid7()` needs 3.14+). No
  table declares `DEFAULT gen_random_uuid()` or any other DB-side default. This is not a style
  preference — the idempotency write order below depends on the application knowing a transfer's
  id *before* the transfer row exists. (ADR-0004)
- **`idempotency_records.transfer_id`'s foreign key must be `DEFERRABLE INITIALLY DEFERRED`.** A
  plain FK rejects the idempotency-record insert immediately, since it happens before the
  referenced transfer row exists (see next point). Get this wrong and the very first successful
  transfer throws a foreign-key violation. (ADR-0002, ADR-0004)
- **Write order inside the one transaction per transfer attempt:** (1) pre-generate the transfer's
  UUIDv7 id, (2) `INSERT` into `idempotency_records` referencing it, (3) lock both wallets
  ascending by `wallet_id`, (4) `INSERT` the `transfers` row with that same id, (5) balance check,
  (6) ledger inserts, (7) status update, (8) commit. All one transaction — a partial version of
  this (e.g. two transactions, or idempotency record written outside the lock) reopens the exact
  race idempotency exists to close. **Note the wallet lock now comes before the transfer insert,
  not after** — an earlier draft had this reversed; real-Postgres testing (CT1/CT7) found that
  order deadlocks, since `INSERT INTO transfers` takes an implicit, unordered `FOR KEY SHARE` lock
  on each referenced wallet as part of its FK check, defeating the ascending-order guarantee the
  explicit lock exists to provide. Locking first means that check has nothing left to contend
  with. (ADR-0002, ADR-0003)
- **Three different validation categories, three different places, do not conflate them:**
  - Malformed body → rejected at the handler boundary by the generated Pydantic models (ADR-0005).
    Service never sees it.
  - Self-transfer / non-positive amount → domain-level invariants, enforced by the `Transfer`
    entity's own constructor/factory (ADR-0006), called by the service *before* any transaction
    opens or any repository call. Nothing persisted.
  - Non-existent wallet → **not** the same as the above. Discovered *inside* the transaction, via
    the locking `SELECT ... FOR UPDATE` returning zero rows — this happens *after* the idempotency
    record is already inserted. On failure, the entire transaction (idempotency record included)
    rolls back and the API returns `404`. (ADR-0002, ADR-0003)
- **`POST /transfers` returns `200` for both `PROCESSED` and `FAILED`.** A `FAILED` transfer
  (insufficient funds) is a correctly handled business outcome, not an HTTP-level error — the
  response body's `status` field carries which terminal state, plus `failureReason` when `FAILED`.
  Reserve non-2xx for actual failures: `404` (wallet doesn't exist), `409` (idempotency key reused
  for a different payload), `503` (bounded retry exhausted), request-shape `4xx` (malformed body /
  self-transfer / non-positive amount). Replaying a `FAILED` key returns the same `200`, never a
  different code than the original request. (ADR-0008)
- **Wallet locks: always ascending `wallet_id` order, regardless of transfer direction.** This is
  what prevents deadlock between two opposite-direction transfers on the same wallet pair — not a
  convention, the actual mechanism. (ADR-0003)
- **Three retryable error classes, exactly:** `lock_timeout`, Postgres's `deadlock_detected`, and a
  connection-pool-acquire timeout. Bounded retry (up to 3 attempts, exponential backoff + jitter,
  `asyncio.sleep()` — never blocking). Insufficient funds is **never** retried — it's terminal.
  `lock_timeout` is set once per transaction (`SET LOCAL`) and covers every lock wait in it,
  including the idempotency-insert's unique-constraint conflict wait, not only the wallet locks.
  (ADR-0003)
- **Size the `asyncpg` connection pool with real headroom** above the concurrency this project
  actually exercises — e.g. `max_size=120` against CT6's 100-concurrent-attempt ceiling. A pool
  that's too small fails at connection-acquire, a different failure mode than a row-lock timeout,
  and needs to hit the same retry/503 path, not surface as an unhandled exception. (ADR-0003)
- **The idempotency-key uniqueness guarantee generalizes to any number of concurrent attempts**,
  not just two top-level requests — a client retry, a client double-fire, and the server's own
  internal bounded-retry attempts for one original request are all just transactions racing the
  same `idempotency_key` unique-constraint insert. No special-casing needed; CT7 proves it under
  the specific case of a client retry landing between the original request's own retry attempts.
  (ADR-0002, ADR-0003)
- **No currency field, anywhere, deliberately.** Every wallet/transfer is assumed to be the same
  unspecified currency. Do not add a `currency` column, conversion logic, or a same-currency check
  — this is an explicitly out-of-scope limitation (`ARCHITECTURE.md`), not a gap to fill.
- **No wallet-creation endpoint.** Wallets come from a flag-gated seed migration only —
  `db/seed/`, applied only when `SEED_DEMO_DATA=true` is passed to `just migrate`, versioned in the
  reserved `V9000`–`V9999` band so it always sorts after real schema migrations. Building a
  `POST /wallets` endpoint is scope this project doesn't call for — don't add one. (ADR-0010)
- **`PENDING` is never durably observable.** A transfer resolves to `PROCESSED` or `FAILED` within
  the same transaction and request that created it. No worker, no queue, no polling endpoint. Any
  future transfer-history read only ever sees a terminal state. (ADR-0008)

## 3. Schema reference (ADR-0004, current)

```
wallets(id UUID PK, balance BIGINT NOT NULL CHECK (balance >= 0), created_at, updated_at)

transfers(id UUID PK, from_wallet_id UUID FK -> wallets, to_wallet_id UUID FK -> wallets,
  amount BIGINT NOT NULL CHECK (amount > 0),
  status TEXT NOT NULL CHECK (status IN ('PENDING','PROCESSED','FAILED')),
  failure_reason TEXT, created_at, updated_at,
  CHECK (from_wallet_id <> to_wallet_id),
  CHECK (status <> 'FAILED' OR failure_reason IS NOT NULL))

ledger_entries(id UUID PK, transfer_id UUID FK NOT NULL -> transfers, wallet_id UUID FK -> wallets,
  type TEXT NOT NULL CHECK (type IN ('DEBIT','CREDIT')), amount BIGINT NOT NULL CHECK (amount > 0),
  created_at)
  -- indexed on wallet_id, transfer_id

idempotency_records(idempotency_key TEXT PRIMARY KEY, request_fingerprint TEXT NOT NULL,
  transfer_id UUID NOT NULL UNIQUE REFERENCES transfers DEFERRABLE INITIALLY DEFERRED, created_at)

-- additional indexes: transfers(from_wallet_id), transfers(to_wallet_id)
```

Migrations: Flyway, `db/migrations/V1__create_wallets.sql` ... `V4__create_idempotency_records.sql`,
via the official `flyway/flyway` Docker image, `just migrate`. Seed data separately in `db/seed/`,
version band `V9000+`, only via `SEED_DEMO_DATA=true just migrate` (ADR-0010).

## 4. Implementation order

Interface-first per ADR-0006 — define each layer's contract before implementing it, then TDD
against that contract. This order is also what makes the parallel split in §5 possible.

1. **Migrations** (`db/migrations/`) — the four tables above. Verify with `just migrate` against
   the `docker-compose.yml` Postgres.
2. **Domain** — `Wallet`, `Transfer`, `LedgerEntry` entities. `Transfer.mark_processed()` /
   `mark_failed(reason)`; only `PENDING -> PROCESSED` / `PENDING -> FAILED` constructible, illegal
   transitions rejected, self-transfer / non-positive-amount rejected at construction. Pure, no I/O.
   → DT1, DT2.
3. **Repository contracts** — async `typing.Protocol` per repository (`WalletRepository`,
   `TransferRepository`, `LedgerRepository`, `IdempotencyRepository`), plus an in-memory fake
   implementing each. The fake is a second real implementation, not a mock — no patching, no
   call-count assertions.
4. **Service** — `TransferService.create_transfer(request) -> Transfer`, built against the
   Protocols (not concrete classes), TDD'd against the fakes. → ST1–ST7.
5. **Repository implementations** — `asyncpg`-backed, thin wrapper methods over a connection pool,
   no ORM. Tested against real Postgres via `testcontainers`, real concurrent connections via
   `asyncio.gather`. → CT1–CT7, LT1–LT4.
6. **API contract** — `openapi/spec.yaml` hand-authored first, models generated via
   `datamodel-code-generator`, drift-checked against FastAPI's own runtime-derived OpenAPI JSON
   (CI-blocking). → E2.
7. **Handlers** — thin FastAPI routes, async, using the generated models for typing. Validation +
   transport mapping only, no business logic, no direct repository access.
8. **End-to-end** — real Postgres, real FastAPI test client. → E1.
9. **Seed migration + `scripts/simulate.py`** (ADR-0010, ADR-0003) — demo tooling, not part of the
   graded core path but named in the ADRs as the interview demo.
10. **`ARCHITECTURE.md`** — a `##` section per top-level module (`handlers`, `services`,
    `repositories`, `domain`), added in the *same PR* as the component, not deferred.

## 5. Suggested agent decomposition

> Not what actually happened — implementation ran sequentially, one step of §4 at a time in a
> single session, reviewable incrementally rather than as several parallel diffs. Left here as a
> record of the option that was considered and explicitly declined, not a description of the
> real history.

Dependency-ordered; agents at the same tier can run in parallel.

| Agent | Owns | Depends on | Gate |
|---|---|---|---|
| A — Schema | `db/migrations/`, `db/seed/`, docker-compose sanity | none | `just migrate` succeeds |
| B — Domain | `domain/` entities + state machine | none | DT1, DT2 green |
| C — Contracts | Repository Protocols, in-memory fakes, `openapi/spec.yaml` skeleton | B | Protocols type-check, fakes satisfy them |
| D — Service | `services/transfer_service.py` | C, B | ST1–ST7 green |
| E — Repository | `repositories/*_asyncpg.py` | A, C | CT1–CT7, LT1–LT4 green |
| F — Handlers/API | `handlers/`, generated models, drift check | D, C | E2 green, `.importlinter` clean |
| G — Verification | E2E tests, `scripts/simulate.py`, `ARCHITECTURE.md` sync, final gate | everything | §7 fully green |

Agent G is the one that should actually run last and holds the "definition of done" checklist below
— don't let any other agent declare the project finished.

## 6. Required test scenarios — full matrix

**Domain (pure, no I/O):**
- DT1 — `mark_processed()`/`mark_failed()` on an already-terminal transfer is rejected.
- DT2 — only `PENDING -> PROCESSED` and `PENDING -> FAILED` are constructible.

**Service (in-memory fake — deterministic race *outcomes*, not real concurrency):**
- ST1 — same key, same fingerprint → cached terminal result, no wallet-locking logic called.
- ST2 — same key, different fingerprint → `409`.
- ST3 — fake raises a retryable error (`lock_timeout` / `deadlock_detected` / pool-acquire
  timeout) on attempt 1, succeeds on attempt 2 → retries and succeeds within the bound.
- ST4 — same retryable error on every attempt → exhausts retries, `503`.
- ST5 — insufficient funds → `FAILED`, zero ledger entries, no retry.
- ST6 — self-transfer / non-positive amount → rejected before any repository call, no idempotency
  record attempted.
- ST7 — non-existent wallet → idempotency insert succeeds first, then the fake's wallet-lock
  reports not-found, everything (including the fake idempotency record) rolls back, `404`.

**Critical section (repository/integration, real Postgres, real concurrent connections):**
- CT1 — N concurrent debits on one wallet exceeding balance: exactly the affordable ones succeed,
  balance never negative.
- CT2 — opposite-direction concurrent transfers on the same wallet pair: both complete, no
  deadlock.
- CT3 — forced lock contention: contender hits `lock_timeout`, retries, succeeds once released.
- CT4 — identical idempotency key, concurrent: exactly one transfer, same result to both callers.
- CT5 — same key, different payload, concurrent: loser gets `409`.
- CT6 — 50–100 concurrent attempts on one wallet (`scripts/simulate.py`): balance reconciles
  exactly.
- CT7 — client retry injected into the gap between the *original* request's own internal retry
  attempts (needs an explicit synchronization point, not a sleep): exactly one transfer, same
  result to both.

**Ledger correctness (repository/integration + end-to-end):**
- LT1 — every `PROCESSED` transfer has exactly 2 ledger entries, one DEBIT one CREDIT, equal
  amounts.
- LT2 — every `FAILED` transfer has zero ledger entries.
- LT3 — global: `sum(CREDIT) == sum(DEBIT)` across the whole ledger after a batch of concurrency
  scenarios.
- LT4 — per-wallet: `balance == initial_balance + sum(CREDIT) - sum(DEBIT)`, checked after
  CT1/CT6.

**End-to-end:**
- E1 — full HTTP round-trip: normal transfer, sequential idempotent replay, each
  validation-boundary rejection.
- E2 — OpenAPI drift check, as a required CI check, not a pytest case.

## 7. Definition of done

- [x] `just ci` green — lint (ruff + mypy + `lint-imports` against `.importlinter`), format-check,
      full test tier (not just the fast fake-backed service tests).
- [x] Every scenario in §6 passes.
- [x] `.importlinter`'s handler → service → repository → domain contract holds — no violations.
- [x] `ARCHITECTURE.md` has a filled-in Executive Summary, System Overview diagram, and a `##`
      section for every top-level module.
- [x] OpenAPI drift check passes — verified locally (`just ci`'s `check-openapi-drift` step);
      will also run in GitHub Actions once the PR is open (branch is pushed).
- [x] `scripts/simulate.py` run manually against `docker-compose.yml` Postgres — balance
      reconciliation passes. Verified live via `just demo` + `just simulate` (and again at
      `--fan-in 100`), same result both times.
- [x] Commit messages are conventional (`feat:`/`fix:`/`docs:`/`test:`/`refactor:`/`chore:`) —
      graded explicitly.
- [ ] AI usage disclosure (tool, how it's generally used, full session transcript or prompt list)
      compiled and added to the PR — do this as you go, not at the end. A draft
      (`AI_USAGE.md`, uncommitted) exists locally; the human is handling the PR's actual AI
      disclosure content directly, with session transcripts attached as PR comments split by
      phase, rather than via that draft file.
- [ ] PR opened into `main` using `.github/pull_request_template.md`, explaining schema design,
      idempotency strategy, concurrency handling, and assumptions/tradeoffs. Deliberately not
      done yet — holding for explicit go-ahead before pushing the branch or opening anything.
- [x] Each ADR's status flipped from Proposed to Accepted only once its own tests in §6 are green
      — don't bulk-flip statuses before that's actually true.

## 8. When you find something this document doesn't cover

Don't guess and don't silently pick a resolution — especially for anything touching money,
transaction boundaries, or the idempotency mechanism. If an ADR is ambiguous, or two ADRs would
conflict in a way not already resolved above, stop and surface it explicitly (a comment in the PR,
or an ADR update) rather than resolving it your own way in code. This is exactly how the
contradictions listed in §2 were originally found — by treating "the design doc doesn't quite say"
as a stop condition, not a place to improvise.

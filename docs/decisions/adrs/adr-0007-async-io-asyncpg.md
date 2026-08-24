---
kind: adr
status: accepted
owner: Kartik Mittal
last_reviewed: 2026-08-24
---

# ADR-0007 — Async I/O end-to-end via asyncpg, not sync routes over a threadpool

- **Status:** Accepted
- **Deciders:** Kartik Mittal (candidate)

## Context

ADR-0005 originally framed FastAPI with synchronous route handlers, specifically to avoid
async/connection-pool complexity while reasoning carefully about transaction boundaries. Per
explicit direction, integration test setup must work without mocks, using `asyncpg` — which has
**no synchronous API at all**. Testing against `asyncpg` while the running service used a
different (sync) driver would mean the tests exercise a different code path than production —
the same drift risk ADR-0005's OpenAPI drift check exists to prevent, just relocated to the data
layer. The more defensible choice is to commit to `asyncpg` end-to-end and correct the earlier
sync-routes framing, since ADR-0005 is still `Proposed`, not yet `Accepted`.

## Decision drivers

- `asyncpg` forces every caller in its chain to be async — there's no partial adoption.
- Tests should exercise the actual repository implementation the service uses at runtime, not a
  parallel one built on a different driver.
- ADR-0003's concurrency design already centers on retry/backoff under lock contention — an async
  event loop doesn't tie up an OS thread for the duration of a lock wait or backoff sleep, a real
  efficiency win under exactly the contention scenarios this project exists to handle correctly.
- FastAPI is async-native; sync `def` routes over its threadpool was a deliberate trade to dodge
  async complexity, but that trade stops making sense once the driver itself forces async.

## Considered options

1. **Sync routes + a sync driver (e.g. `psycopg3`), `asyncpg` used only in tests.** Rejected:
   tests would exercise a different driver/code path than the running service.
2. **Sync routes + `asyncpg` invoked via `asyncio.run()` per request from inside a sync handler.**
   Rejected: fights the grain — spinning up an event loop per request inside a threadpool worker
   is more complex than just writing async routes, with none of async's actual benefit (each
   threadpool worker still blocks for the request's full duration either way).
3. **Async FastAPI routes end-to-end, `asyncpg` as the sole Postgres driver, repository
   `Protocol`s defined as async.** *Chosen.*

## Decision

- All route handlers are `async def`. Repository `Protocol`s (ADR-0006) are async (e.g. `async def
  get_for_update(...) -> Wallet`), implemented against `asyncpg` directly — a connection pool
  (`asyncpg.create_pool`) with thin wrapper methods, not a full ORM, keeping the repository layer
  the same thin shape already decided.
- Bounded retry/backoff (ADR-0003) uses `asyncio.sleep()` for its jittered backoff — never a
  blocking `time.sleep()`, which would defeat the entire point inside an async route.
- The locked critical section (ADR-0003) is an `async with connection.transaction():` block
  (`asyncpg`'s async transaction context manager) — same semantics (lock → check → write →
  commit/rollback), now non-blocking.
- Testing: `pytest-asyncio` drives async tests. Repository-layer and end-to-end tests connect to
  the real `testcontainers`-provisioned Postgres through the **same** `asyncpg` pool machinery the
  app itself uses — no mocking of the database layer anywhere in that tier, full stop. The
  in-memory fake used for service-layer tests (ADR-0006) implements the same async `Protocol` —
  its methods are `async def`, trivially awaiting plain in-memory logic — it is a **fake** (an
  alternate real implementation of the contract), not a **mock** (nothing there patches or stubs
  the real repository, or asserts on call counts/arguments instead of behavior).

## Consequences

**Good:** tests genuinely exercise the same driver and code path as the running service — no
drift between what's tested and what's deployed. Non-blocking I/O under lock contention and
retry backoff is a direct, material win for exactly the concurrency scenarios this project is
designed around. FastAPI's async-native design is used as intended instead of worked around.

**Bad / risks:** `await` now has to thread through every layer (handler → service → repository) —
more upfront ceremony than sync routes would have needed, though this now correctly reflects a
real I/O-bound driver rather than being complexity introduced for its own sake. CI must provide a
runner with Docker socket access for `testcontainers`-backed tests. This is a separate risk from,
and additional to, the one ADR-0001 already flagged: ADR-0001 could only confirm that the
`runs-on: [actions_runner_dev_new]` runner's *existence* is unverified from this fork — this ADR
separately notes that even if it exists, its Docker socket access is equally unverified (corrected
on review — a prior version of this ADR conflated the two risks as if ADR-0001 had already covered
this one). `requirements-dev.txt` gains `asyncpg`, `pytest-asyncio`, and `testcontainers[postgres]`.

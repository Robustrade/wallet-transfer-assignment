# Wallet Transfer Service — Complete Walkthrough

A study guide for understanding the whole service end to end: the endpoints, how a request
travels from the client through each layer down to the database, and how every hard requirement
(idempotency, concurrency, ledger integrity, state transitions) is solved. Read top to bottom
once and you should be able to answer any question about the design.

---

## 1. What the service does, in one paragraph

It moves money between wallets. A client calls `POST /transfers` with a source wallet, a
destination wallet, an amount, and an `idempotencyKey`. The service atomically debits the source,
credits the destination, records two ledger entries (one DEBIT, one CREDIT), and marks the
transfer `PROCESSED` — or, if the source lacks funds, marks it `FAILED` and changes nothing. The
same `idempotencyKey` sent twice never moves money twice. Concurrent transfers on the same wallet
never corrupt the balance or overdraw it.

**Stack:** Java 17, Spring Boot 3.2, plain JDBC with explicit SQL (no ORM), PostgreSQL, Flyway
for migrations, Testcontainers for tests.

---

## 2. How many endpoints (APIs) did we build?

Three. One is the assignment's core requirement; two are small read endpoints for verification
and demos (the assignment lists these as optional enhancements).

| Method & path | Purpose | Required? |
|---------------|---------|-----------|
| `POST /transfers` | Create (or idempotently replay) a wallet-to-wallet transfer | Core requirement |
| `GET /transfers/{id}` | Look up a single transfer's status/details | Optional |
| `GET /wallets/{id}` | Read a wallet's current balance | Optional |

Everything below is mostly about the one endpoint that matters: `POST /transfers`.

---

## 3. The layers (and the one rule for each)

The code is split into four layers, each in its own package under `com.wallet`. The point of the
split is that each layer has exactly one responsibility and never reaches into another's:

```text
handler     (com.wallet.handler)    HTTP in, HTTP out. No business logic, no SQL.
service     (com.wallet.service)    Business logic, idempotency, transaction boundaries.
repository  (com.wallet.repository) Database access only. Explicit SQL. No business rules.
domain      (com.wallet.domain)     The entities and their own validity rules.
```

- **Handler** validates the request shape, calls one service method, maps the result (or an
  exception) to an HTTP status. That's all.
- **Service** is where the actual workflow lives — checking for a duplicate, locking wallets,
  moving money, writing the ledger, flipping the status.
- **Repository** only talks to the database. It takes and returns domain objects and runs SQL. It
  contains zero business rules (no "is the balance enough" logic — that's the service's job).
- **Domain** objects protect their own invariants (a `Transfer` refuses an illegal state
  transition; money is always normalized to 4 decimal places).

---

## 4. The request lifecycle — one `POST /transfers`, traced through every layer

This is the most important section. Follow a single successful request all the way down.

**Client sends:**

```json
POST /transfers
{ "idempotencyKey": "abc123", "fromWalletId": "w1", "toWalletId": "w2", "amount": 100 }
```

**Step 1 — Handler (`TransferController.create`).**
Spring deserializes the JSON into a `TransferRequest` record and runs bean validation on it
(`@NotBlank` keys, `@DecimalMin` so amount > 0, `@Digits(integer=15, fraction=4)` so an amount
with too many decimal places is rejected rather than silently rounded). The wallet id strings are
parsed into UUIDs by the shared `RequestIds.parseUuid` helper. The controller then calls
`transferService.transfer(...)` and nothing else.

**Step 2 — Service orchestration (`TransferService.transfer`).**
This method is deliberately **not** transactional itself. It does three things:

1. **Cheap guard checks** — source ≠ destination, amount positive. Fail fast with a clear 400.
2. **Compute the request fingerprint** — a SHA-256 of `from|to|amount`. This is how we later
   detect "same idempotency key, but a different payload."
3. **Fast-path idempotency lookup** — `findByIdempotencyKey`. If a transfer with this key already
   exists, we don't do any work; we return it as a replay (or raise a conflict — see §6). If it
   does not exist, we hand off to `TransferExecutor`.

**Step 3 — The atomic unit of work (`TransferExecutor.execute`, `@Transactional`).**
This is the single database transaction where everything that must be all-or-nothing happens, in
this exact order:

1. **Lock both wallets** with `SELECT ... FOR UPDATE`, always in ascending wallet-id order (not
   the order the caller passed them). Locking in a fixed global order is what prevents deadlocks
   between opposite-direction transfers (see §7). `lockById` throws `WalletNotFoundException` if a
   wallet doesn't exist, so there's no separate existence check.
2. **Insert the transfer row** as `PENDING`. (Insert happens *after* the locks — doing it before
   causes a deadlock; that story is in §7.) The `UNIQUE` constraint on `idempotency_key` is the
   real concurrency guarantee here.
3. **Check the source balance under the lock.** If insufficient → mark the transfer `FAILED`,
   record the reason, and **commit** (a failed transfer is a real, recorded business outcome, not
   an error). Return.
4. If sufficient → **debit** the source, **credit** the destination, **insert the two ledger
   entries** (DEBIT on source, CREDIT on destination), and mark the transfer `PROCESSED`.
5. **Commit.** Every write above — both balance updates, both ledger rows, the status change —
   lands together or not at all.

**Step 4 — Back up through the layers.**
The service returns a `TransferResult` (the transfer plus a "was this a replay?" flag). The
controller turns it into a `TransferResponse` and picks the status code: **201** for a freshly
executed transfer, **200** for an idempotent replay.

**Repository layer throughout** — every database touch above goes through `WalletRepository`,
`TransferRepository`, or `LedgerEntryRepository`, each running explicit parameterized SQL via
`NamedParameterJdbcTemplate`. No ORM, so the locking and the exact SQL are visible and
intentional.

---

## 5. The data model (3 tables) and why

```text
wallets          id, balance (NUMERIC(19,4), CHECK >= 0), timestamps
transfers        id, idempotency_key (UNIQUE), request_fingerprint, from_wallet_id (FK),
                 to_wallet_id (FK), amount (CHECK > 0), status (CHECK in PENDING/PROCESSED/FAILED),
                 failure_reason, timestamps, CHECK(from <> to)
ledger_entries   id, transfer_id (FK), wallet_id (FK), entry_type (CHECK in DEBIT/CREDIT),
                 amount (CHECK > 0), created_at, UNIQUE(transfer_id, entry_type)
```

The guiding principle: **every rule that can be a database constraint, is one.** The database —
not application code — guarantees that balances can't go negative, an amount can't be zero or
negative, a transfer can't point a wallet at itself, a ledger entry can't reference a transfer
that doesn't exist, and a transfer can't have two DEBITs or two CREDITs. This is the single
biggest thing the evaluation rubric looks for.

Note there is **no `idempotency_records` table** even though the assignment suggests one — that's
the one deliberate deviation, explained in §8.

---

## 6. Idempotency — how duplicates are made safe

> **In plain terms.** You've seen this in real life: you tap your card, the machine hangs, and
> you're not sure if it went through — so you tap again, and you're terrified you just paid twice.
> Idempotency is the promise that you *won't* be charged twice. The `idempotencyKey` is like a
> reference number you staple to your request: "this is payment **abc123**." The first time the
> service sees abc123 it does the work. Every time after, it recognizes the number and just hands
> back the same receipt — it does **not** move the money again. The client can safely retry as
> many times as it wants; only the first one actually does anything.
>
> Why would a client send the same request twice at all? Network timeouts, a lost response, an
> automatic retry, a user double-clicking "Send." In a distributed system this is normal, not
> rare — so the service has to be built to expect it.

**The mechanism:** the `transfers` row *is* the idempotency record. Two columns carry it:
`idempotency_key` (with a `UNIQUE` constraint) and `request_fingerprint` (the SHA-256 of the
payload).

**Three cases, three outcomes:**

- **New key** → execute normally, return **201**.
- **Same key, same payload** → return the original transfer unchanged, **200**. No money moves a
  second time.
- **Same key, different payload** (e.g. someone reused the key but changed the amount) → the
  fingerprints don't match → **409 Conflict**. We refuse to guess which request they meant.

**Why it's safe under concurrency:** suppose two identical requests arrive at the same instant and
both get past the fast-path lookup (both see "no existing transfer"). They both try to insert with
the same `idempotency_key`. The `UNIQUE` constraint lets **exactly one** succeed; the other gets a
`DuplicateKeyException`, which the service catches, re-reads the now-committed transfer, and
returns as a replay. The database does the arbitration atomically — we never rely on application
timing.

> **In plain terms.** Think of the `UNIQUE` rule as a bouncer at a door who will only ever let one
> person named "abc123" inside. Even if two people named abc123 rush the door at the exact same
> moment, the bouncer admits one and turns the other away. The one turned away doesn't get an
> error in the client's face — the service quietly says "oh, abc123 is already inside," fetches
> that person's result, and returns it. Either way the client gets one clean answer and the money
> moves once. We don't try to be clever with timing in our own code; we let the database be the
> bouncer, because it's the one thing that can make that decision perfectly every time.

**Why it survives restarts:** it's a committed database row, not an in-memory cache. Kill the
process mid-flight and the guarantee is still intact when it comes back.

---

## 7. Concurrency — no double-spend, no deadlock

**The risk:** two transfers debiting the same wallet at the same time could both read the old
balance, both decide there's enough money, and both subtract — overdrawing the wallet (a classic
read-then-write race / double-spend).

> **In plain terms (the double-spend).** Imagine a wallet has $100, and two withdrawals of $80
> each hit it at the exact same moment. Both look at the balance, both see $100, both think "plenty
> of money," and both subtract $80 — leaving **-$60**. The wallet just spent money it didn't have.
> That's the bug we have to prevent.

**The solution — pessimistic row locks.** Before touching any balance, the transaction does
`SELECT ... FOR UPDATE` on the wallet rows. That makes concurrent transfers on the same wallet
**take turns**: the second one blocks until the first commits, then reads the *already-updated*
balance and makes its decision against the truth. No overdraft is possible. This is proven by a
test that races 20 threads draining one wallet and asserts the balance never goes negative.

> **In plain terms (the lock).** A lock is like a single-occupancy room with a key. To change a
> wallet's balance, a transfer has to take the wallet's key and go in; anyone else who wants that
> same wallet has to **wait outside** until the first one comes out. So in the example above, the
> first withdrawal takes the key, sees $100, takes $80, leaves $20, and gives the key back. *Only
> then* does the second withdrawal get in — and now it sees the real $20, realizes $80 won't fit,
> and is correctly rejected. They can't both be "in the room" at once, so they can't both spend the
> same money. The cost is that the second one waits a moment — that's a price worth paying for
> never losing money.

**The deadlock we hit and fixed (good story for the discussion):** a transfer locks two wallets.
If transfer A→B locks A then B, while transfer B→A locks B then A, they can deadlock — each
holding what the other wants. We avoid it by **always locking in ascending wallet-id order**,
regardless of direction, so both transactions grab the lower id first and one simply waits.

> **In plain terms (the deadlock).** Picture two people in a narrow doorway, each waiting for the
> other to step aside first — nobody moves, forever. That's a deadlock: transfer A→B is holding
> wallet A and waiting for wallet B, while transfer B→A is holding wallet B and waiting for wallet
> A. Each is stuck holding exactly what the other needs. The fix is a simple shared rule everyone
> obeys: **always grab the lower-numbered wallet first.** Once both transfers reach for the same
> wallet first, one of them simply gets it and the other waits its turn — no standoff is possible.
> (We hit a second, sneakier version of this too, which is in the technical paragraph below; the
> takeaway is the same — fix the *order* things are locked in, and the standoff disappears.)

There was a subtler deadlock too: originally we inserted the transfer row *before* taking the
locks. Because `transfers` has foreign keys to `wallets`, Postgres takes an implicit shared lock
on the referenced wallet rows during that insert — and concurrent inserts then deadlocked trying
to upgrade those shared locks to `FOR UPDATE`. The fix: **lock the wallets first, insert the
transfer second.** Both deadlock scenarios have regression tests.

**Why pessimistic and not optimistic locking?** Wallet transfers are write-heavy and contend on
hot wallets. Row locks give predictable "wait your turn" behavior; optimistic locking would cause
retry storms under that same contention. The tradeoff is documented.

---

## 8. The double-entry ledger and the state machine

> **In plain terms (the ledger).** A ledger is just a logbook of every movement of money — like a
> bank statement that can never be erased. "Double-entry" is the centuries-old accounting rule that
> money never appears or vanishes, it only *moves*: every time it leaves one place it must arrive
> in another. So a single $100 transfer writes **two** lines in the logbook — "−$100 from wallet
> A" and "+$100 to wallet B." Add those two lines together and you get zero, which is the proof
> that nothing was created or destroyed, it just changed hands. If you ever wanted to audit the
> system, you'd add up every line in the logbook and it should always balance to zero. The wallet
> balances are the "current total"; the ledger is the permanent history of how they got there.

**Double-entry:** every processed transfer writes exactly two `ledger_entries` — a DEBIT against
the source and a CREDIT against the destination, same amount. They always net to zero. This is
guaranteed structurally: the `UNIQUE(transfer_id, entry_type)` constraint makes it physically
impossible to write two debits or two credits for one transfer, and both rows are written in the
same transaction as the balance changes, so they can't partially apply. A test asserts the two
entries sum to zero.

**State machine:** a transfer is `PENDING` at creation and moves to exactly one terminal state —
`PROCESSED` (money moved) or `FAILED` (e.g. insufficient funds). The transitions are guarded in
the `Transfer` domain object itself: trying to move out of a terminal state throws. So even a
buggy retry can't flip a `PROCESSED` transfer to `FAILED` or re-process it. `FAILED` is a
committed outcome, not a rollback — the transfer is recorded, just with no ledger entries and no
balance change.

---

## 9. Error handling — every failure maps to the right HTTP status

A single `GlobalExceptionHandler` maps everything to one consistent JSON error shape, with the
correct status code:

| Situation | Status |
|-----------|--------|
| Transfer created (PROCESSED, or FAILED for insufficient funds) | 201 |
| Idempotent replay of an existing key + identical payload | 200 |
| Validation failure (blank field, non-positive or over-precise amount, malformed UUID) | 400 |
| Malformed / empty JSON body | 400 |
| Unknown wallet or unknown transfer id | 404 |
| Same idempotency key reused with a different payload | 409 |
| Unsupported HTTP method / content type | 405 / 415 |
| Anything unexpected | 500 (logged server-side, generic message to client) |

Two important details: **insufficient funds is a 201, not an error** — it's a successful request
with a `FAILED` business outcome. And the handler extends Spring's `ResponseEntityExceptionHandler`
so framework-level problems (bad JSON, wrong method) keep their proper 4xx codes instead of being
swallowed into a 500 — and the catch-all never leaks an exception's internal message to the
caller.

---

## 10. Deliberate decisions & tradeoffs (be ready to defend these)

1. **No separate `idempotency_records` table.** The assignment suggests four tables; we used
   three. The `transfers` row, with its `idempotency_key` (UNIQUE) and `request_fingerprint`
   columns, *is* the idempotency record. **Why:** the result a replay must return is just the
   transfer, which we already store — a separate table would duplicate it and create a second row
   that must be kept in sync (a drift-bug risk). The uniqueness guarantee is identical either way.
   **When the separate table would win:** if idempotency had to be a generic layer across many
   different endpoints, or needed to replay the exact original HTTP response byte-for-byte, or had
   its own expiry lifecycle. None apply here.

2. **Pessimistic locking over optimistic.** Predictable blocking under contention vs. retry
   storms. (See §7.)

3. **Insufficient funds = committed `FAILED`, not an HTTP error.** It's a business outcome worth
   recording, not a malformed request.

4. **Plain JDBC, no ORM.** So the locking and exact SQL are explicit and visible — the whole
   point of this assignment is the transaction/locking behavior, which an ORM would hide.

5. **Stored balance, not balance-derived-from-ledger.** Simpler reads and a natural lock target.
   The ledger still exists as the immutable audit trail and is what you'd reconcile against.

---

## 11. Testing — what proves all of the above

23 tests, run against a real PostgreSQL via Testcontainers (not mocks), covering:

- happy-path transfer + balances + balanced ledger entries
- idempotent replay (same result, no double-debit)
- idempotency conflict (same key, different payload → 409)
- insufficient funds → FAILED, no ledger, no balance change
- unknown wallet → 404
- **concurrency:** 20 threads draining one wallet → never overdraws, ledger stays consistent
- **deadlock avoidance:** concurrent A→B and B→A both complete
- **concurrent duplicate key:** many threads, same key → exactly one transfer ever created
- domain unit tests for the state machine
- HTTP-level tests for the status codes (including malformed JSON → 400, wrong method → 405)

---

## 12. Likely questions from the panel, with short answers

- **"What happens if the same request is sent twice?"** The unique `idempotency_key` means the
  second one returns the original transfer (200) and moves no money. (§6)
- **"What if the first request commits but the response is lost?"** The retry with the same key
  finds the committed transfer and replays it. The guarantee is a committed DB row, so it's safe
  across restarts. (§6)
- **"Are two concurrent debits on the same wallet safe?"** Yes — `SELECT ... FOR UPDATE`
  serializes them; the second reads the updated balance. Proven by a 20-thread test. (§7)
- **"Is there a read-then-write race?"** No — the balance is only read *after* the row lock is
  held. (§7)
- **"Can ledger rows exist without a transfer?"** No — foreign key. Can a transfer have two
  debits? No — `UNIQUE(transfer_id, entry_type)`. (§5, §8)
- **"Why no `idempotency_records` table?"** §10, point 1 — and the tradeoff of when you'd want it.
- **"Where are the transaction boundaries?"** One transaction, in `TransferExecutor.execute`,
  wrapping the locks + balance updates + ledger writes + status change. (§4)

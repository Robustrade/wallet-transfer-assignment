# Wallet Transfer Service — Coding Assignment ## Overview Build a small service that supports **wallet-to-wallet transfers**. The goal of this assignment is to evaluate your ability to design a **reliable transactional system** with correct handling of: - idempotency - concurrency - ledger consi

pasted

I have a 5h take-home: `POST /transfers` between wallets, must be
idempotent, concurrency-safe, double-entry ledger, Go + Postgres. Before I
start — what's the actual hard part here so I don't waste time on the wrong
thing?

- Before you answer search online blogs and articles, what are the hurdles they would face on these kind of projects. Where the flow silently breaks.
  The full assignment is attached for your ref.

01:08

Synthesized real-world failure modes into actionable guidance
Synthesized real-world failure modes into actionable guidance
Good research gathered. Here's the real breakdown — not the surface-level stuff, but where implementations silently go wrong at each layer.

The Actual Hard Parts (in order of how badly they bite you)

1. Idempotency is a race condition itself
   The obvious approach — "check if key exists, if not process, then store" — is broken by design. Two identical requests arriving within milliseconds both hit the idempotency check simultaneously, both find no existing key, and both proceed to execute. The naive SELECT-then-INSERT creates a TOCTOU race: even with a UNIQUE constraint, the second request fails with a constraint violation rather than being gracefully handled as a duplicate.
   DZone

The fix: Use INSERT ... ON CONFLICT DO NOTHING to make the check-and-claim atomic. If the RETURNING clause yields no rows, the key already existed — fetch its status with SELECT ... FOR UPDATE.
DZone

But there's a subtler failure too: the timing and the lack of atomic commitment of the entire operation — lock acquisition, database write, deduplication record write — can lead to duplicate action even when the idempotency key mechanism itself didn't fail. In other words, if your app crashes after inserting the idempotency record but before committing the transfer, you have a "processed" key with no actual transfer. The idempotency record and the transfer must be committed in the same transaction.
The Coders Blog

2. The TOCTOU on balance checks
   Checking balance before acquiring the lock creates a classic time-of-check-to-time-of-use (TOCTOU) race. Two concurrent withdrawals could both pass the balance check, then both execute, overdrawing the account. The balance check must happen inside the transaction after the FOR UPDATE lock is acquired.
   freeCodeCamp

This catches almost everyone. The code looks correct in sequential testing. It only breaks under concurrent load.

3. Deadlocks from inconsistent lock ordering
   Deadlocks occur when two (or more) transactions wait on each other for data in a circular fashion. If Transaction A locks wallet_1 then wallet_2, and Transaction B locks wallet_2 then wallet_1, both are now waiting for the other to commit or roll back.
   DEV Community

The fix: Always acquire row locks in a consistent deterministic order — e.g., always lock the wallet with the lower UUID first. One line of code, eliminates the whole class of deadlock.

4. Stored balance vs. ledger-derived balance — the consistency trap
   You have two choices: derive balance by summing ledger entries, or maintain a balance column updated atomically during each transfer. Both work, but mixing them is lethal.

The balance column on accounts is a denormalized cache for fast reads, and the ledger sum is the ground truth that validates it. If you maintain both (which is the fast-read pattern), they must be updated in the same transaction as the ledger write, or you'll have a balance that disagrees with the ledger under crashes or partial failures. Concurrent transfers can cause an account to go negative, or concurrent balance updates can clobber each other.
freeCodeCamp
Paul Gross's Blog

5. The "in-flight" idempotency state — the missing PENDING status
   Most people model idempotency records as binary: exists or doesn't. But what happens when request A is mid-processing and request B arrives with the same key? An idempotency key isn't just "present" or "absent" — it has three life stages: Null (new, lock it and proceed), PENDING (currently processing on another thread, tell the user to wait with 409 Conflict), and Completed (return saved response immediately).
   DEV Community

Without the PENDING state, two concurrent requests with the same key can both pass the "does this exist?" check before either commits.

6. Floating point money — the silent corruption
   Using float64 for amounts is a trap. Use int64 (store cents/smallest unit) or decimal/numeric in Postgres. With one line of code like UPDATE accounts SET balance = balance - 100, you've created a system that can silently lose millions through floating point imprecision, a server crash, or a race condition — with no audit trail and no way to prove what happened.
   freeCodeCamp

Priority Map for Your 5 Hours
Risk Where it silently breaks Fix
🔴 Critical Idempotency TOCTOU INSERT ... ON CONFLICT DO NOTHING inside the same tx as the transfer
🔴 Critical Balance check before lock Always check balance after SELECT FOR UPDATE
🔴 Critical Deadlock from lock ordering Lock wallets in consistent ID order
🟡 High Partial commit (idempotency record without transfer) One DB transaction for both
🟡 High In-flight concurrent duplicate Track PENDING state on idempotency records
🟢 Medium Balance vs. ledger drift Update both in same tx, or derive balance from ledger
🟢 Low Float money Use NUMERIC/int64 cents from day one
The One Architectural Decision That Cascades Everything
Your biggest design choice is: does the idempotency record go in the same DB transaction as the transfer?

If yes (recommended): you get atomicity for free, the idempotency record is your transfer's existence proof, and rollbacks are clean.

If no (separate systems, e.g. Redis for idempotency + Postgres for transfer): you now have a distributed two-phase commit problem — you must handle the case where Postgres committed but Redis update failed, or vice versa. For a 5-hour take-home, stay in one Postgres transaction.

The flow that survives all of this:

BEGIN
INSERT INTO idempotency_records ON CONFLICT DO NOTHING → if 0 rows, SELECT existing → return cached
SELECT wallet_1, wallet_2 FOR UPDATE (lower ID first)
Check balance after lock
INSERT transfer (PENDING)
INSERT two ledger entries
UPDATE wallet balances
UPDATE transfer → PROCESSED
UPDATE idempotency_records → store response
COMMIT
Everything inside one transaction. Crash anywhere = full rollback = clean retry.

Okay what are the things i sould focus on building only, so I can finish this project within 5h. MOSTLY im the one going to code, im following TDD(RED, BLUE, GREEN) also i will use ai auto completion, But still I would have to code faster so i would have to initially decide on folder structure and code architecture, i wouldnt be having time to do this at the end, Once all this done, i would be asking ai to crate a proepr docker compose file and make file with readme to demostrate everything.

what are the things i shouldnt go across like shouldnt go that way,
jwhat are the things i should focus onfirst how to start it and how to end it proeprly so i can finsih it within given time
01:15

Architected pragmatic TDD roadmap prioritizing essentials over perfection
Architected pragmatic TDD roadmap prioritizing essentials over perfection
Your 5h Build Plan
What NOT to touch (time traps)
No JWT/auth — the spec doesn't ask for it
No wallet creation API — seed wallets via migration SQL, not an endpoint
No transfer history / balance API — optional in spec, skip unless you finish early
No retry queue / background workers — keep it synchronous
No Redis — pure Postgres idempotency, one less dependency
No ORMs — raw sqlx, you'll waste time fighting struct tags and losing control of FOR UPDATE queries
No optimistic locking — pessimistic (SELECT FOR UPDATE) is simpler to reason about and test in 5h
No Swagger/OpenAPI docs — a good README covers it
Folder structure — decide now, don't touch later
wallet-service/
├── cmd/
│ └── server/
│ └── main.go # wire everything, start HTTP
├── internal/
│ ├── domain/
│ │ └── models.go # Transfer, Wallet, LedgerEntry, states as consts
│ ├── handler/
│ │ └── transfer.go # HTTP decode → call service → encode response
│ ├── service/
│ │ └── transfer.go # ALL business logic lives here
│ ├── repository/
│ │ └── transfer.go # ALL SQL lives here, nothing else
│ └── db/
│ └── db.go # sqlx setup, ping
├── migrations/
│ └── 001_init.sql # schema, seed wallets
├── docker-compose.yml
├── Makefile
└── README.md
No pkg/, no api/, no config/ package — you don't have time.

The one transaction function that IS your service
Write this mental model first, everything else scaffolds around it:

go
// service/transfer.go — the entire transfer in one tx
func (s *Service) CreateTransfer(ctx context.Context, req CreateTransferRequest) (*Transfer, error) {
// 1. INSERT idempotency record ON CONFLICT DO NOTHING
// 2. If 0 rows inserted → SELECT existing → return cached result
// 3. BEGIN TX
// 4. SELECT wallet_from, wallet_to FOR UPDATE ORDER BY id ← deadlock prevention
// 5. Check balance after lock
// 6. INSERT transfer (PENDING)
// 7. INSERT ledger_entry debit
// 8. INSERT ledger_entry credit
// 9. UPDATE wallet balances
// 10. UPDATE transfer → PROCESSED
// 11. UPDATE idempotency_record → store result
// 12. COMMIT
}
Every test you write maps to one of these steps failing.

TDD order — Red/Blue/Green sequence
Do these in order. Each one is ~20-30min with autocomplete helping on the Blue step.

# Test What it proves

1 Transfer debits from wallet, credits to wallet Happy path works
2 Transfer creates exactly 2 ledger entries Double-entry correct
3 Same idempotency key twice → same result, 1 transfer row Idempotency works
4 Insufficient balance → error, no ledger entries, no balance change Rollback clean
5 Two goroutines debit same wallet concurrently → balance never negative Concurrency safe
That's it. 5 tests. If you have time, add: transfer to self → error, unknown wallet → error.

Schema — write this first, everything compiles against it
sql
CREATE TABLE wallets (
id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
balance NUMERIC(19,4) NOT NULL DEFAULT 0 CHECK (balance >= 0),
created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE transfers (
id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
idempotency_key TEXT NOT NULL UNIQUE,
from_wallet_id UUID NOT NULL REFERENCES wallets(id),
to_wallet_id UUID NOT NULL REFERENCES wallets(id),
amount NUMERIC(19,4) NOT NULL CHECK (amount > 0),
status TEXT NOT NULL DEFAULT 'PENDING',
created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE ledger_entries (
id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
transfer_id UUID NOT NULL REFERENCES transfers(id),
wallet_id UUID NOT NULL REFERENCES wallets(id),
type TEXT NOT NULL, -- DEBIT | CREDIT
amount NUMERIC(19,4) NOT NULL,
created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Seed wallets for testing
INSERT INTO wallets (id, balance) VALUES
('00000000-0000-0000-0000-000000000001', 1000),
('00000000-0000-0000-0000-000000000002', 1000);
No separate idempotency_records table — transfers.idempotency_key is your idempotency store. The UNIQUE constraint IS your atomic guard. Simpler schema, fewer joins, same correctness.

Time budget
0:00–0:20 Schema SQL + migrations setup
0:20–0:40 Folder structure, main.go wiring, db.go, empty interfaces
0:40–1:40 Domain models + repository layer (all SQL functions)
1:40–3:10 Service layer TDD — all 5 tests, Red→Blue→Green
3:10–3:40 Handler layer (thin, just HTTP encode/decode)
3:40–4:00 Manual smoke test with curl
4:00–4:30 Ask AI: docker-compose, Makefile, README
4:30–5:00 Buffer — fix whatever broke, cleanup
The two things that will kill your time if you let them

1. Test database setup — use testcontainers-go or just a real local Postgres with a TEST_DB_URL env. Don't mock the DB — your concurrency test needs a real DB. Decide this before writing test #1.

2. Getting the idempotency tx boundary wrong — the idempotency key claim (INSERT ... ON CONFLICT) must happen outside the main transfer transaction but the result must be stored inside it. The pattern:

claim key (own statement, not in tx) → if already claimed, return cached
open tx → do everything → store result on the transfer row → commit
If you put the key claim inside the transfer tx, a rollback wipes the claim and you retry forever. Think this through before you type line one of service code.

Wallet Transfer Service — Implementation Plan Context This is a timed (3h) coding assignment evaluated by a senior-engineer rubric (evaluation_guide.md) plus SonarQube + Copilot review. The graded core is transactional correctness: idempotency, concurrency-safe debits, double-entry ledger, safe stat

pasted

Okay dont giv eme code now, jsut help me with the decisions, i will ask you if i need code snippets.
next, i have a plan here based on my own experince and reseach : (pasted)
here i have few doubts on these:

For the balance check — optimistic (version column, retry onconflict) or pessimistic (SELECT ... FOR UPDATE)? It's money.
if I lock from then to, and another transfergoes to→from, that's a classic deadlock. I'll lock both rows in a singlestatement ordered by wallet id so the lock order is always the same. Any reasonthat's wrong?
Idempotency key. My first thought: look up the key, if it existsreturn the old response, else do the transfer and store it. But that's acheck-then-act race if two identical requests land together. How does this getdone in production — Stripe-style?
Insufficient funds — do I just return 422 and not write anything, orpersist a FAILED transfer row?
I keep wanting to add a stringer enum + driver.Valuer/Scannerfor the statuses so the DB stores readable text. Worth it in 5h?
I don't have time for exhaustive tests. If I write three tests, whichthree actually prove the assignment?
Show more
01:21

Resolved to address six architectural decisions directly and concisely
Resolved to address six architectural decisions directly and concisely
Good questions. Direct answers only.

1. Pessimistic or optimistic locking?

Pessimistic, no debate. Optimistic means you detect the conflict after the work, retry, and need a loop. Under high contention on the same wallet, you get cascading retries and the retry logic becomes its own bug surface. Pessimistic with SELECT FOR UPDATE blocks the second writer at the DB level, no application retry loop needed. For a 5h submission where the reviewer is explicitly checking concurrency correctness, the simpler mechanism that's obviously correct beats the clever one that requires reasoning about retry loops.

2. Locking both rows in one ordered statement — any reason that's wrong?

Your instinct is correct and the approach is sound. The canonical way is SELECT ... FROM wallets WHERE id = ANY($1) ORDER BY id FOR UPDATE — pass both IDs in one query, Postgres locks them in index order, deadlock is structurally impossible regardless of which direction concurrent transfers go. This is the right answer and worth saying explicitly in your PR note — reviewers look for it.

3. Idempotency — the Stripe-style pattern

Stripe's actual approach, which matches your plan's design: insert-first, not lookup-first.

The sequence is:

INSERT INTO idempotency_records ... ON CONFLICT DO NOTHING
Check how many rows were inserted
If 0 rows → you lost the race → SELECT the existing record → check its status → replay or 409
If 1 row → you own this key → proceed with the transfer
The UNIQUE constraint makes the insert atomic. There is no window between check and act. The lookup-first pattern you described is the broken version — this insert-first pattern is what fixes it.

One more thing from your plan that's correct: the idempotency record must be committed separately from the transfer transaction. If you put the insert inside the transfer tx, a rollback wipes the claim and concurrent retries all think they own the key. Claim the key first in its own statement, then open the transfer tx.

4. Insufficient funds — 422 with no write, or persist FAILED?

Persist a FAILED transfer row, always. Three reasons:

The assignment explicitly lists PENDING → FAILED as a required state transition — a row that never existed can't demonstrate that
The idempotency requirement says the same key must return the same result on replay. If you wrote nothing, you can't replay a 422 — you'd re-execute the business logic, which means a race where funds appear later could turn a "failed" key into a "processed" one on retry
The rubric is evaluating state machine correctness; a FAILED row is the proof
The FAILED row + stored 422 response on the idempotency record is what makes failure replayable and idempotent. No write means broken idempotency on the failure path.

5. Stringer enum + Valuer/Scanner — worth it in 5h?

Yes, but only if you timebox it hard. It's worth it because the rubric your plan references explicitly calls out "Go templating and code generation tools (e.g., Stringer)" as a skill signal. If you skip it, you're leaving a free point on the table.

The effort is small if you do it right at commit 3 (domain layer, before anything depends on it): define the int iota types, write //go:generate stringer, run it once, commit the generated file. The Valuer/Scanner is maybe 20 lines shared across all enum types. The danger is doing it later when things already depend on string literals — that's where it eats time.

6. The three tests that actually prove the assignment

In order of what the rubric cares about:

Happy path + double-entry invariant — balances move correctly, exactly 2 ledger entries created, SUM(credits) - SUM(debits) == 0 for that transfer. This proves the core requirement.
Idempotency replay — same key submitted twice, assert: one transfer row in DB, both calls return identical response body, balance moved exactly once. This proves the hardest behavioral requirement.
Concurrent debit race — N goroutines all debit the same wallet simultaneously with distinct keys, assert: final balance is exactly initial - (N × amount), never negative, total ledger entries = N × 2. This is the proof of SELECT FOR UPDATE working. No other test demonstrates concurrency correctness.
Everything else (404 unknown wallet, 422 mismatch key) is nice-to-have. If you only ship these three and they're solid, the rubric core is covered.

okay i have started the project with basic docker compose and other things, i want to skip this templating jst og twith stringer and finsih fast so i want to structure and finsih it asap i have very less time what is the best way, i want you to think about my plan and code so far i ahve coded this way you know how im writing it and waht the plan is so iw ant you to structure folder, which im going to do later however but if you can give me write away based ont he proejct scope and my exisitng code i would start the structuring now itself, i want you to consider solid and dry principals, mock testing shoudl be possibel as i have used interfaces mostly, its going to be test driven arch, so mostly evryheing is going to be domain and their test int he same folder give me an folder structure please.

01:23

Synthesized architecture requirements and devised scalable folder structure
Synthesized architecture requirements and devised scalable folder structure
No files uploaded — going purely off your plan and what you've described. Here's the structure:

wallet-service/
│
├── cmd/
│ └── server/
│ └── main.go # wire: config → pool → migrate → router → serve
│
├── internal/
│ │
│ ├── config/
│ │ └── config.go # env vars: DATABASE_URL, PORT
│ │
│ ├── platform/
│ │ ├── postgres/
│ │ │ └── db.go # pgxpool.New + ping
│ │ └── migrate/
│ │ └── migrate.go # golang-migrate, embed.FS, run on startup
│ │
│ ├── apperr/
│ │ └── errors.go # sentinel errors: ErrInsufficientFunds,
│ │ # ErrWalletNotFound, ErrIdempotencyConflict,
│ │ # ErrFingerprintMismatch, ErrInProgress
│ │
│ ├── domain/ # pure types, zero I/O, zero framework
│ │ ├── money.go # type Amount int64, Validate() error
│ │ ├── wallet.go # type Wallet struct
│ │ ├── transfer.go # type Transfer struct + TransferState (int iota)
│ │ ├── transferstate_string.go # GENERATED — commit this, do not edit
│ │ ├── ledger.go # type LedgerEntry struct + EntryType (int iota)
│ │ ├── entrytype_string.go # GENERATED — commit this, do not edit
│ │ ├── enum_sql.go # shared Valuer/Scanner for all int-iota enums
│ │ └── domain_test.go # unit: Amount validation, state transitions,
│ │ # String() output, Valuer/Scanner round-trip
│ │
│ ├── transfer/
│ │ ├── repository.go # Repository interface (port) + pgx impl (adapter)
│ │ ├── repository_test.go # real Postgres: FOR UPDATE order, ledger insert,
│ │ │ # balance update, WithinTx
│ │ ├── service.go # Service struct — owns the full workflow
│ │ ├── service_test.go # behavioral: happy path, idempotency replay,
│ │ │ # insufficient funds, concurrency race
│ │ ├── handler.go # thin chi handler: decode → validate → service
│ │ └── handler_test.go # httptest: 201, 400, 409, 422, 404 shapes
│ │
│ ├── idempotency/
│ │ ├── repository.go # IdempotencyRepository interface + pgx impl
│ │ └── repository_test.go # insert-first ON CONFLICT, replay, status update
│ │
│ └── httpapi/
│ ├── router.go # chi.NewRouter, mount /transfers, middleware
│ └── errors.go # apperr → HTTP status + JSON body (one place)
│
├── migrations/
│ ├── 000001_init.up.sql
│ └── 000001_init.down.sql
│
├── testsupport/
│ └── pg.go # testcontainers: start Postgres, run migrations,
│ # return \*pgxpool.Pool; truncate helper
│
├── docker-compose.yml
├── Makefile
├── go.mod
├── go.sum
└── README.md
Key decisions baked in
Why idempotency/ is its own package, not inside transfer/

The idempotency repository has its own DB table and its own interface. If it lives inside transfer/, the service has to import its own package for the interface — circular. Separate package, service depends on both interfaces via constructor injection. Clean DIP.

Why tests live next to the code, not in a tests/ folder

repository_test.go is in package transfer_test (black-box) — it can only call exported methods. Same for service and handler. The domain_test.go is the only one that's white-box (package domain) because you're testing unexported validation logic. This is idiomatic Go TDD.

testsupport/pg.go is shared

All three real-DB test files (repository_test.go, service_test.go, idempotency/repository_test.go) call the same testsupport.NewPostgres(t) helper. One container, truncated between tests. DRY.

apperr/ sits below everything

domain doesn't import apperr. Domain returns its own validation errors (plain errors.New). apperr is for service-layer sentinel errors that the handler layer maps to HTTP codes. This keeps the dependency arrow pointing inward only.

httpapi/errors.go is the only place that knows HTTP status codes

Handler calls service, gets an apperr, passes it to errors.MapToHTTP(w, err). Nothing else touches http.StatusUnprocessableEntity etc. DRY, and SonarQube won't flag repeated magic numbers.

The import chain (no cycles)
main → httpapi → transfer/handler → transfer/service → transfer/repository
→ idempotency/repository
→ domain
→ apperr
→ errors.go → apperr
domain imports nothing internal. apperr imports nothing internal. Everything else flows one direction.

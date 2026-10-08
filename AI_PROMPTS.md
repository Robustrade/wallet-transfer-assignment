# AI user prompt history

Exact user task prompts exported from the available local session segments for this chat, through the assignment audit request. This is the prompt-history alternative allowed by ASSIGNMENT.md, not a full assistant/tool transcript. UI context, system/developer instructions, and tool output are excluded. Historical prompts do not prove their requested work was completed; see AUDIT.md for current evidence. Timestamps below are recorded UTC timestamps.

## Prompt 1 — 2026-10-07T19:17:56.977Z

These are the decisions I want to keep consistent unless we find a concrete issue:

- Go 1.21
- PostgreSQL
- `database/sql` with `github.com/jackc/pgx/v5/stdlib`
- standard `net/http`
- `context.Context` through handler -> service -> repository -> DB
- clean flow: HTTP handler -> service -> repository -> PostgreSQL
- `int64` minor units for money; no floating point
- one DB transaction for the complete financial transfer
- `SELECT ... FOR UPDATE` for wallet balance protection
- deterministic wallet lock ordering to reduce deadlock risk
- DB uniqueness as the final protection for idempotency
- double-entry ledger: exactly one DEBIT and one CREDIT for a successful transfer
- explicit states: `PENDING`, `PROCESSED`, `FAILED`
- behavior-focused tests
- for new business behavior, follow Red -> Blue -> Green: write a failing behavior test first, implement the smallest correct change, then refactor with tests green
- real PostgreSQL integration tests for locking, transactions, rollback, and concurrency
- keep the design small and explainable; don’t add infrastructure unless a requirement needs it

If you think one of these decisions is unsafe or inconsistent with the current code, call it out with the concrete failure scenario before changing it.


## Prompt 2 — 2026-10-07T19:26:28.284Z

Read  [ASSIGNMENT.md](ASSIGNMENT.md)  fully before changing application code.\
\
Before implementation, create a short \`DESIGN.md\` that captures the decisions I’m making for this service.\
\
Cover:\
\
\- problem statement\
\- expected behavior of \`POST /transfers\`\
\- request/response contract\
\- stored-balance strategy\
\- double-entry ledger invariant\
\- transaction boundary\
\- idempotency behavior\
\- retry behavior\
\- concurrency strategy\
\- transfer state transitions\
\- expected side effects\
\- failure modes\
\- observability approach\
\- testing strategy\
\
Document these specific decisions:\
\
1 Wallet balance is stored in 'wallets' and updated in the same transaction as the ledger.\
2 Money uses \`int64\` minor units so arithmetic is exact.\
3 Wallet rows are protected with PostgreSQL 'SELECT ... FOR UPDATE'.\
4 When two wallets are locked, they are acquired in deterministic wallet-ID order.\
5 Idempotency uses a database unique constraint as the final concurrency guarantee.\
6 A successful transfer creates exactly one DEBIT and one CREDIT ledger entry.\
7 Valid transfer transitions are PENDING -> PROCESSED\` and \`PENDING -> FAILED\
8 Expected business failure and infrastructure/database failure are different:\
&#x20;  -if a transfer row has been created and an expected business rule fails, persist \`FAILED\` where the chosen workflow allows it;\
&#x20;  -if the database transaction itself fails, rollback instead of pretending a 'FAILED' row was safely persisted.\
9 For a completd idempotent request, a retry after a lost HTTP response must return the original result without repeating side effects.\
10 PostgreSQL is used because it is the preferred persistence option in  [ASSIGNMENT.md](ASSIGNMENT.md) \
\
For testing, follow the requested Red -> Blue -> Green discipline for new behavior:\
\- Red: add a failing behavior test\
\- Blue: implement the smallest correct change\
\- Green: refactor with tests still passing\
\
Also check the Git state. The work should be on a branch created from 'main', not directly on 'main'. If the repo is already on an appropriate feature branch, keep it.\
\
Do not implement the application in this step.\
\
At the end, show me the design decisions and any requirement that is still ambiguous.


## Prompt 3 — 2026-10-07T19:32:42.151Z

Read 'ASSIGNMENT.md' first and use it as the source of functional requirements.

The repo is empty, so start by creating the Go 1.21 service foundation.

I want to keep the application small and explicit, so use:

- 'net/http' instead of a web framework
- 'database/sql'
- 'github.com/jackc/pgx/v5/stdlib'
- env-based configuration
- PostgreSQL

Create a structure along these lines:

cmd/server/
internal/
    config/
    database/
    domain/
    service/
    repository/
        postgres/
    handler/
        http/
migrations/
tests/

For this step, only add:

- 'go.mod'
- main entry point
- config loading
- PostgreSQL connection and pool setup
- HTTP server
- '/health'
- graceful SIGINT/SIGTERM shutdown

Don’t add transfer logic yet.

Use 'context.Context' properly for startup/shutdown and DB operations.

When done, run:

go fmt ./...
go vet ./...
go test ./...

Fix anything that fails.

Then give me a short summary of:
- what you created
- any assumption you made
- anything that will matter when we add transactions later

---


## Prompt 4 — 2026-10-07T19:35:10.405Z

Now add the domain layer before we write persistence or HTTP code.

I want the domain types to stay independent of PostgreSQL and HTTP.

Add:

## Wallet
- ID
- Balance
- CreatedAt
- UpdatedAt

## Transfer
- ID
- IdempotencyKey
- FromWalletID
- ToWalletID
- Amount
- Status
- CreatedAt
- UpdatedAt

## LedgerEntry
- ID
- WalletID
- TransferID
- Type
- Amount
- CreatedAt

Use explicit types/constants for:

TransferStatus:
PENDING
PROCESSED
FAILED

LedgerEntryType:
DEBIT
CREDIT

For money, use 'int64' minor units. I don’t want 'float32'/'float64' anywhere in the money path because exact arithmetic matters here.

Add validation for:
- amount > 0
- source != destination
- valid transfer state
- valid ledger entry type

For state transitions, keep them explicit. The only valid transitions should be:

PENDING -> PROCESSED
PENDING -> FAILED

Don’t expose a generic “set status to anything” path if we can avoid it.

Add unit tests for the domain rules.

Run fmt, vet, and tests when done.


## Prompt 5 — 2026-10-07T19:37:04.520Z

Create the PostgreSQL migrations.

I want these tables:

wallets
transfers
ledger_entries
idempotency_records

Design the schema around invariants, not just fields.

## wallets

Need:
- id
- balance
- created_at
- updated_at

Use PostgreSQL 'BIGINT', compatible with Go 'int64'.

Add a defensive constraint so a stored wallet balance cannot be negative:

CHECK (balance >= 0)

## transfers

Need:
- id
- idempotency_key
- from_wallet_id
- to_wallet_id
- amount
- status
- created_at
- updated_at

Add FKs for source/destination wallets.

Add useful constraints such as:

CHECK (amount > 0)
CHECK (from_wallet_id <> to_wallet_id)
CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED'))

## ledger_entries

Need:
- id
- wallet_id
- transfer_id
- entry_type
- amount
- created_at

A processed transfer should have exactly one DEBIT and one CREDIT.

Use DB constraints for the invariants PostgreSQL can enforce directly:

CHECK (amount > 0)
CHECK (entry_type IN ('DEBIT', 'CREDIT'))
UNIQUE (transfer_id, entry_type)

The 'UNIQUE (transfer_id, entry_type)' constraint prevents two DEBIT rows or two CREDIT rows for the same transfer.

Document one business assumption explicitly: only a 'PROCESSED' transfer represents money movement and therefore receives the two ledger entries. A 'FAILED' transfer must not create debit/credit ledger entries because no money moved.

## idempotency_records

I want the DB to be the final source of truth for key uniqueness.

Store:
- idempotency key
- deterministic request fingerprint
- resulting transfer reference
- timestamps / status fields if required by the implementation

The fingerprint must let us detect this case:

same key + different from/to/amount

as a conflict rather than treating it as a retry.

Add the indexes we actually need for:
- wallet lookup
- idempotency-key lookup
- transfer lookup
- ledger lookup by transfer

Create up/down migrations.

Before finishing, review the schema for:
- referential integrity
- duplicate ledger prevention
- duplicate idempotency prevention
- indexes that support the queries we plan to use

Explain any constraint you intentionally leave to the service layer instead of the DB.


## Prompt 6 — 2026-10-07T19:38:45.472Z

Now define repository contracts.

I want SQL details contained in the repository layer, while the service owns orchestration and transaction boundaries.

Create the operations needed for:

## Wallet
- get by ID
- get two wallets with write locks inside a transaction
- update balance

## Transfer
- create
- get by ID
- update status
- retrieve the transfer associated with an idempotency record/result

## Ledger
- create entry
- list entries by transfer ID

## Idempotency
- create/reserve a key
- get by key
- attach the final transfer/result

Also add a transaction abstraction that lets the service execute one callback/unit of work against a single '*sql.Tx'.

I don’t want separate transactions hidden inside repository methods because the balance changes, ledger entries, transfer state, and idempotency result need one atomic boundary.

Keep the abstraction small. If an interface doesn’t buy us anything, don’t add it just for style.

Run fmt, vet, and tests afterward.



## Prompt 7 — 2026-10-07T19:42:14.996Z

Implement the repository layer with 'database/sql' and 'github.com/jackc/pgx/v5/stdlib'.

Every DB call should accept 'context.Context'.

For wallet locking, use an active transaction and 'FOR UPDATE'.

I want both wallets locked in deterministic ID order to reduce the classic opposite-transfer deadlock:

T1: A -> B
T2: B -> A

Use a query pattern like:

SELECT ...
FROM wallets
WHERE id IN ($1, $2)
ORDER BY id
FOR UPDATE;

Important: lock order and business role are different things. After reading the rows, the code still needs to map the actual source and destination correctly.

For errors:
- distinguish 'sql.ErrNoRows' from a database failure
- when needed, inspect '*pgconn.PgError' with 'errors.As'
- recognize PostgreSQL SQLSTATE '23505' ('unique_violation')
- recognize SQLSTATE '40P01' ('deadlock_detected') and '40001' ('serialization_failure') as transaction-level failures
- wrap errors so callers keep context, but don’t leak raw SQL/driver details to HTTP

Important PostgreSQL behavior: after a normal statement returns a constraint error such as '23505', that transaction is aborted until rollback (unless a savepoint is used). Don’t write repository/service code that catches '23505' and then continues issuing queries in the same failed transaction.

Don’t start or commit transactions inside normal repository methods. The service should own that.

Add repository tests where they are useful, but keep real locking/transaction verification for integration tests.

Run fmt, vet, and tests.



## Prompt 8 — 2026-10-07T19:45:04.581Z

Now implement the core service method.

I want the financial mutation to happen inside one PostgreSQL transaction.

Before changing the service, add/adjust failing behavior tests for the success path, insufficient balance, and rollback. Then implement the smallest change that makes them pass.

The intended flow is:

1. validate request
2. begin transaction
3. reserve/check idempotency key
4. lock source and destination wallets
5. verify both wallets exist
6. create transfer as PENDING once the request can be represented safely
7. evaluate the balance/business rule
8. for an expected business failure after PENDING exists, transition PENDING -> FAILED and persist the original failed result consistently
9. otherwise debit source
10. credit destination
11. create DEBIT ledger entry
12. create CREDIT ledger entry
13. mark transfer PROCESSED
14. attach the final result to the idempotency record
15. commit

Be precise about failure semantics:

- input validation failures before a transfer exists do not need a fake transfer row
- an expected business failure may be represented by 'PENDING -> FAILED' if we have created the transfer
- an infrastructure/DB error that invalidates the transaction must rollback; do not try to claim a FAILED state was persisted inside a transaction that did not commit
- retries of a committed FAILED idempotent result should return that original result rather than create a new transfer

If an operation fails in a way that requires rollback, rollback the whole transaction.

The invariants I care about are:

- no source debit without destination credit
- no destination credit without source debit
- no balance change without both ledger entries
- no successful transfer with only one ledger entry
- no PROCESSED status before the financial work is complete
- no duplicate transfer side effect for the same idempotency key

Use a safe deferred rollback pattern so all early returns are covered.

Don’t add retry loops around the whole transaction yet. First make the transaction correct and observable.

Add tests for:
- successful transfer
- insufficient balance
- missing wallet
- rollback on a mid-transaction failure if we can inject one cleanly

After implementation, summarize the exact transaction boundary and which statements run under the wallet locks.



## Prompt 9 — 2026-10-07T19:48:57.689Z

Now focus only on idempotency.

The behavior I want is:

### First request
- claim/reserve the key
- process the transfer
- persist the final transfer reference/result

### Same key + same request
- return the original result
- no second debit/credit
- no extra transfer
- no extra ledger rows

### Same key + different request
- return an idempotency conflict

Build the request fingerprint from the business identity of the request:

fromWalletId
toWalletId
amount

Don’t make correctness depend on:

SELECT key
if not found -> INSERT

because two requests can pass the read concurrently.

Use the PostgreSQL unique key as the final arbitration point, but handle it in a PostgreSQL-safe way.

Prefer a pattern such as:

INSERT INTO idempotency_records (...)
VALUES (...)
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING ...;

Then:

- if the insert returns a row, this request owns the key and can continue
- if it returns no row, load the existing idempotency record and compare the request fingerprint
- if the fingerprint differs, return an idempotency conflict
- if the existing request already completed, return its original transfer/result
- if the conflicting transaction was still in progress, rely on PostgreSQL conflict/locking behavior so we do not execute the financial side effect twice

Do not intentionally trigger '23505' and then continue querying in the same transaction, because PostgreSQL marks that transaction failed after the error.

Please review the transaction ordering carefully here. I want to avoid a case where one request sees the same key but cannot distinguish:
- completed original request
- an original request that is still being resolved
- conflicting payload

If the current idempotency table needs a small state field such as 'IN_PROGRESS', 'COMPLETED', or 'FAILED' to make the behavior clearer, propose it first and explain why it is needed before changing the schema. If keeping the idempotency reservation and transfer in one atomic transaction makes an externally visible 'IN_PROGRESS' state unnecessary, explain that instead.

Use Red -> Blue -> Green for the idempotency cases.

Add tests for:
- first request
- identical retry
- retry after the original transfer committed but the caller did not receive/keep the HTTP result (simulate by invoking the same request again)
- conflicting retry
- committed FAILED result retry, if FAILED results are persisted
- balance moves once
- ledger rows are created once
- concurrent same-key requests

The retry-after-commit case should prove that a network/client retry cannot repeat financial side effects.



## Prompt 10 — 2026-10-07T19:51:28.512Z

Now review the double-entry ledger path.

For a successful transfer of amount X:

source      DEBIT   X
destination CREDIT  X

The two entries must:
- reference the same transfer
- have the same amount
- point to the correct wallets
- use different entry types

The ledger is part of the transaction, not an after-the-fact log.

If either ledger insert fails, the balance updates must roll back.

Keep/verify a DB uniqueness rule that prevents duplicate DEBIT or CREDIT rows for one transfer.

Add tests that prove:
- exactly two entries for PROCESSED transfer
- one DEBIT and one CREDIT
- equal amounts
- correct wallets
- no ledger entries after rollback
- idempotent retries don’t create more ledger rows

Also add one assertion/helper that verifies the transfer-level ledger is balanced.


## Prompt 11 — 2026-10-07T19:54:15.368Z

Add:

POST /transfers

Request:

{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}

Use only 'net/http' and 'encoding/json'.

Keep the handler thin:

decode
transport validation
call service
map error/result
write JSON

No SQL, DB transaction handling, wallet locking, balance math, idempotency orchestration, or ledger writes in the handler.

Return consistent JSON errors.

Map at least:
- malformed JSON -> 400
- invalid request -> 400
- wallet not found -> 404
- insufficient balance -> suitable client error
- idempotency conflict -> 409
- unexpected DB/internal error -> 500

Don’t leak SQL or internal error details to clients.

Add handler tests for the mapping and response shape.

Also verify the handler returns the same logical transfer result for an idempotent retry and does not hide a persisted 'FAILED' transfer outcome if that is part of the service contract.

Follow Red -> Blue -> Green for new handler behavior.



## Prompt 12 — 2026-10-07T19:55:52.706Z

Add a simple seed path for local development.

I only need predictable wallets for manual testing:

wallet_1 = 10000
wallet_2 = 5000
wallet_3 = 5000

Amounts are minor units.

Prefer a SQL seed file or development-only migration. Don’t add wallet CRUD endpoints just to prepare test data.

Make it safe to rerun if practical.

---

# Prompt 11 — Add behavior tests around the service contract

Add tests around business behavior rather than implementation details.

Cover:

### Success
- PROCESSED result
- source decreased by amount
- destination increased by amount
- one DEBIT
- one CREDIT

### Insufficient funds
- no partial balance update
- no ledger rows
- transfer not incorrectly marked PROCESSED

### Validation
- amount <= 0
- source == destination
- source missing
- destination missing

### Idempotency
- same key/same payload returns original result
- balances move once
- ledger is written once
- same key/different payload returns conflict

Prefer table-driven tests where it makes the cases clearer, but don’t force table-driven style where setup differs too much.



## Prompt 13 — 2026-10-07T19:57:02.771Z

Add tests around business behavior rather than implementation details.

Cover:

#Success
- PROCESSED result
- source decreased by amount
- destination increased by amount
- one DEBIT
- one CREDIT

# Insufficient funds
- no partial balance update
- no ledger rows
- transfer not incorrectly marked PROCESSED

# Validation
- amount <= 0
- source == destination
- source missing
- destination missing

# Idempotency
- same key/same payload returns original result
- balances move once
- ledger is written once
- same key/different payload returns conflict

Prefer table-driven tests where it makes the cases clearer, but don’t force table-driven style where setup differs too much.

## Prompt 14 — 2026-10-07T19:58:53.067Z

Now add integration tests against a real PostgreSQL instance using Testcontainers.

Use the PostgreSQL Testcontainers module if it fits the project:

github.com/testcontainers/testcontainers-go
github.com/testcontainers/testcontainers-go/modules/postgres

I don’t want mocks for:
- 'FOR UPDATE'
- transaction rollback
- DB uniqueness
- foreign keys
- concurrent transaction behavior

Test setup should:
1. start PostgreSQL
2. wait for readiness
3. run migrations
4. seed only the data each test needs
5. run the test
6. clean up

Keep tests isolated so ordering doesn’t matter.

If there is a clean way to share container startup across tests without making test state shared use it.



## Prompt 15 — 2026-10-07T20:04:26.488Z

Add an integration test for:

wallet_1 balance = 100

concurrently:
wallet_1 -> wallet_2 = 80
wallet_1 -> wallet_3 = 80

Expected outcome:
- only one succeeds
- the second transaction sees the committed/locked balance and fails for insufficient funds
- wallet_1 never goes negative
- total successful debit is 80, not 160
- ledger rows only exist for the successful transfer

Use goroutines plus channels / 'sync.WaitGroup' to coordinate the calls.

Don’t use arbitrary sleeps as the main synchronization strategy.

At the end, assert the final DB state directly.

This test should prove that the DB row lock, not a Go process-level mutex, protects the wallet.



## Prompt 16 — 2026-10-07T20:06:57.297Z

Add an integration test where several goroutines send the exact same request at once:

key = abc123
wallet_1 -> wallet_2
amount = 100

Verify:
- one logical transfer
- one debit
- one credit
- exactly two ledger rows
- same transfer/result returned to duplicate callers
- one idempotency record for the key

This needs to exercise the real PostgreSQL uniqueness/transaction behavior.

If it fails because of a race, fix the idempotency design rather than weakening the assertions.



## Prompt 17 — 2026-10-07T20:09:42.315Z

Review the current lock acquisition for:

T1: A -> B
T2: B -> A

I expect deterministic wallet-ID lock ordering to remove the simple circular wait where each transaction locks its source first.

Check that the current query and code actually guarantee consistent order under PostgreSQL.

Also check that source/destination mapping remains correct after sorting/locking.

Add an integration test for opposite-direction concurrent transfers.

I know deterministic ordering reduces deadlock risk but doesn’t mean PostgreSQL can never report a deadlock from every possible access pattern. If there are remaining cases, document them rather than hiding them.

Don’t add a global Go mutex.


## Prompt 18 — 2026-10-07T20:22:20.816Z

Review the current lock acquisition for:

T1: A -> B
T2: B -> A

I expect deterministic wallet-ID lock ordering to remove the simple circular wait where each transaction locks its source first.

Check that the current query and code actually guarantee consistent order under PostgreSQL.

Also check that source/destination mapping remains correct after sorting/locking.

Add an integration test for opposite-direction concurrent transfers.

I know deterministic ordering reduces deadlock risk but doesn’t mean PostgreSQL can never report a deadlock from every possible access pattern. If there are remaining cases, document them rather than hiding them.

Don’t add a global Go mutex.

## Prompt 19 — 2026-10-07T20:25:48.090Z

Review the actual isolation behavior we’re using.

PostgreSQL defaults to 'READ COMMITTED'.

For this transfer path, the important protection is the locking read:

SELECT ... FOR UPDATE

plus a single transaction and uniqueness constraints.

Check whether the current default isolation is sufficient.

Only set 'sql.TxOptions.Isolation' explicitly if there is a real correctness or predictability reason.

Give me a short explanation of:
- isolation level used
- what 'FOR UPDATE' protects
- what the idempotency unique key protects
- what the ledger unique key protects
- what can still cause a deadlock
- how we surface/handle transaction failures

Make code changes only if justified by that review.



## Prompt 20 — 2026-10-07T20:27:00.138Z

Add structured logging with 'log/slog'.

I want enough context to trace a transfer without logging sensitive DB configuration.

Useful fields:
- transfer_id
- idempotency_key
- from_wallet_id
- to_wallet_id
- status

Useful events:
- request accepted for processing
- duplicate idempotent request
- insufficient balance
- transfer committed
- transaction rolled back / failed
- idempotency conflict

Avoid noisy per-query logging.

Pass the logger as a dependency instead of relying on uncontrolled globals.


## Prompt 21 — 2026-10-07T20:29:50.744Z

Review startup/shutdown and connection handling.

Check:
- DB ping/readiness on startup
- 'SetMaxOpenConns'
- 'SetMaxIdleConns'
- 'SetConnMaxLifetime'
- 'SetConnMaxIdleTime'
- HTTP read/write/idle timeouts
- graceful HTTP shutdown
- SIGINT/SIGTERM handling
- DB close on shutdown
- request context propagation

Use reasonable defaults or env config. Don’t turn this into a configuration framework.



## Prompt 22 — 2026-10-07T20:31:27.009Z

Review the full codebase as if you’re reviewing my PR.

Please look specifically for correctness issues, not cosmetic rewrites.

Check:

### Go
- context propagation
- '%w' error wrapping
- 'errors.Is' / 'errors.As'
- row/resource cleanup
- transaction rollback paths
- unnecessary interfaces
- unclear package dependencies

### PostgreSQL
- PostgreSQL transactions and row-level locks
- transaction boundary
- 'FOR UPDATE'
- lock order
- useful indexes
- FK/unique constraints
- duplicate-key handling

### Financial invariants
- atomic debit/credit
- no double spend
- exactly two ledger rows for success
- idempotent retries have no side effects
- rollback leaves no partial financial state

### Concurrency
- same-wallet spending
- same idempotency key
- opposite-direction transfers
- deadlock handling

Run:

go fmt ./...
go vet ./...
go test ./...
go test -race ./...

Fix real issues and explain why each fix matters.

Don’t introduce new abstractions unless they solve a concrete problem found in the review.



## Prompt 23 — 2026-10-07T20:34:09.441Z

Write the README from the code that actually exists.

Include:

## Overview
What the service does.

## Stack
- Go 1.21
- PostgreSQL
- 'database/sql'
- 'github.com/jackc/pgx/v5/stdlib'
- 'net/http'

## Architecture

HTTP Handler
    -> Service
        -> Repository
            -> PostgreSQL

## Data model
Explain:
- wallets
- transfers
- ledger_entries
- idempotency_records

## Transaction boundary

Document the real order of operations:

BEGIN
idempotency reservation/check
lock wallets
validate balance
create PENDING transfer
update balances
insert DEBIT/CREDIT
mark PROCESSED
store final idempotency result
COMMIT

and rollback behavior.

Also document the 'FAILED' path separately:
- expected business failures can transition a persisted 'PENDING' transfer to 'FAILED'
- infrastructure/database failures that invalidate the transaction are rolled back
- 'FAILED' transfers do not create DEBIT/CREDIT ledger entries because no money moved

## Concurrency
Explain:
- PostgreSQL transactions and row-level locks
- 'SELECT ... FOR UPDATE'
- deterministic wallet lock ordering
- why process-local mutexes are not the consistency mechanism

## Idempotency
Explain:
- unique key
- request fingerprint
- same request retry
- conflicting payload
- concurrent duplicate behavior

## Testing
Explain:
- domain/service tests
- HTTP tests
- PostgreSQL integration tests
- concurrency tests

## Local setup
Add the exact env vars and commands needed to run it.

## Tradeoffs
Document deliberate decisions, including why PostgreSQL is a good fit for the transactional and locking requirements.

Keep this concise and technical.



## Prompt 24 — 2026-10-07T20:35:38.613Z

Read 'ASSIGNMENT.md' again and audit the implementation against it.

For each of these, report 'PASS', 'PARTIAL', or 'MISSING' and point to the relevant code/test:

POST /transfers
request validation
idempotency key handling
same result on retry
duplicate side-effect prevention
wallet balance correctness
double-entry ledger
exactly two ledger entries
DEBIT/CREDIT correctness
PENDING / PROCESSED / FAILED
safe state transitions
atomic transaction
rollback
concurrency safety
double-spend prevention
retry safety
clean layers
error handling
behavior tests
concurrency tests
documentation-first design note
Red -> Blue -> Green testing discipline
retry after lost/unknown client response
Git branch created from main
PR AI-usage note and transcript/prompt-history reference

Don’t mark PASS based on intent; verify it in code/tests.

Fix PARTIAL/MISSING items that are in scope.

Also list any deliberate deviations from the technical document so the PR is transparent. PostgreSQL itself is not a deviation because it is the preferred database in 'ASSIGNMENT.md'.

Run the full test suite after fixes.



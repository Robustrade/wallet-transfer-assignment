# Wallet Transfer Service — Design

This note describes the service as it is built, and the analysis that settled the design. It is the contract for API behavior, the books, idempotency, concurrency, and the tests.

The service is a Go HTTP API in front of PostgreSQL 16. A transfer moves a positive amount, in minor units of one implicit currency (INR), from one wallet to another. The same idempotency key and body produce that movement once. A retry returns the stored result. Concurrent debits of one wallet cannot overdraw it. Every committed success has a matching debit and credit, and every wallet obeys:

```text
balance = opening_balance + credits − debits
```

## How it was built

Work followed the assignment’s documentation-first order, then the schema and layering were revised once the rules were concrete.

1. **Plan.** Read the assignment and the CI skeleton (Go 1.24, golangci-lint, PostgreSQL available). Chose a stored balance plus a double-entry ledger, one database transaction as the atomic boundary, an idempotency claim before any wallet lock, and row locks ordered by wallet id.
2. **Design note.** Wrote the first version of this document before application code: contract, failure modes, idempotency, concurrency, and the test plan.
3. **Schema, from scratch.** Drafted tables, then threw the draft away and redesigned them by questioning each column and trigger. The migration that shipped is `migrations/001_init.sql` only.
4. **Model, repository, service, handler.** Entities and state transitions first, then SQL, then the transfer workflow, then HTTP. Business rules that had landed in repositories were moved into the service. The handler keeps transport checks and maps outcomes it cannot know before the service runs.
5. **Concurrency proof.** After the workflow existed, we checked Read Committed plus row locks against Serializable and against a Go mutex, then added tests for two racers, ten racers, one shared key, opposite directions, disjoint pairs, and mixed readers and writers.

## Decisions, and the analysis behind them

### Stored balance, and why `opening_balance` exists

Two ways to know a balance were on the table: sum the ledger on every read, or store `wallets.balance` and update it inside the transfer transaction.

The stored balance is what concurrent debits lock. A derived balance would still need a lock on the source wallet, or a serializable retry loop, to stop a double spend. The row is the obvious lock target.

`opening_balance` was questioned as a test-only column. It is not. Opening funds are not a ledger entry. A fake opening credit would have no matching debit, and the ledger would no longer be pairs that belong to transfers. Without a baseline, summing credits and debits cannot explain a wallet that started above zero. `opening_balance` is that baseline. It is set once, equal to the initial `balance`, and a `BEFORE UPDATE` trigger rejects any later change. Transfers update `balance` only.

The ledger then answers “which transfers changed this wallet.” The current balance is the opening baseline plus that sum. We do not store a running balance on each ledger row, and we do not materialize “balance after transfer N.” Walking entries in order can reconstruct that later. The service and a deferred trigger both check the equation before a changing transaction can commit.

### One currency, implicit INR

The assumption is a single currency, INR, with amounts as `int64` minor units. Floats are rejected.

We designed a `currencies` catalog and a `currency_code` on wallets, transfers, and ledger rows, tied together with composite foreign keys so one transfer could not mix INR and USD. That chain is real, and it is also the reason it was removed. The assignment moves one currency. The composite keys existed so PostgreSQL would accept the foreign keys, not because a second currency was in use. They made every insert harder to read and did not change transfer behavior.

There is no currency column. Adding another currency later is a migration. Until then, every amount in the database is INR minor units, and there is no conversion.

### One transaction is the atomic boundary

Claim, locks, balance updates, ledger rows, status change, and the stored HTTP response commit together or not at all. A crash before commit leaves no transfer, no ledger rows, and no idempotency record. A retry is a new first attempt.

`PENDING` is a real status. The row is inserted as `PENDING` and moved to `PROCESSED` or `FAILED` before commit. Under Read Committed, callers never see the in-flight row. There is no recovery worker. Nothing outside this database has been touched, so there is nothing to resume.

We considered a durable `PENDING` plus a worker for crashes mid-flight. That design fits a transfer that has already called another system. This one has not. Losing the uncommitted attempt is the safe outcome.

### Idempotency: what is stored, and what decides a retry

The key is claimed with `INSERT … ON CONFLICT DO NOTHING` before any wallet lock. A unique-violation error aborts the PostgreSQL transaction even if the application catches it, so conflict handling cannot be a caught `23505`. `ON CONFLICT DO NOTHING` is the mechanism that lets the same transaction continue and read the existing row.

`Claim` does not compare hashes. It only answers whether this transaction inserted the key. The service compares hashes after a conflict:

- same key, same hash → return the stored status and body, including the original `transferId`
- same key, different hash → `409`, and the stored row is left as it is

The hash is lowercase hex SHA-256 of the canonical JSON of `amount`, `fromWalletId`, and `toWalletId`, with keys sorted and no extra whitespace. The key itself is the lookup, not part of the hash. `encoding/json` map marshaling produces that order for this fixed set.

`request_body` stores that same canonical JSON. We asked whether the hash made the body redundant. The hash is what the comparison uses. The body stays so the claimed payload can be read without reversing SHA-256. Replay does not parse it to decide.

We dropped `idempotency_records.transfer_id`. The relationship already runs the other way: `transfers.idempotency_key` is unique and references the claim. The stored response body already contains `transferId`. A second pointer would be filled in after the transfer exists and would not change claim, replay, or mismatch. One direction is the 1:1 link.

We also dropped a check that `request_hash` matches `^[0-9a-f]{64}$` and a check that `request_body` is a JSON object. Those reject malformed rows. They do not prove the hash matches the body, and they are not what makes a retry safe. `NOT NULL`, the primary key, and the stored response are.

Response code and body are both null while the claim is in flight, and both set when the outcome is known. A check constraint requires that pair. `StoreResponse` updates only a row whose `response_code` is still null, so a stored outcome is not overwritten.

### Insufficient funds consumes the key. Unknown wallet and credit overflow do not.

Both wallets exist and the debit cannot be paid: that is a financial answer. It is committed as `FAILED` with `failure_reason = insufficient_funds`, no ledger rows, and no balance change. The idempotency row stores `422` and the response body in the same commit. A later deposit does not make the same key succeed; a retry replays that outcome. The client sends a new key to try again.

A missing wallet is not a financial answer. The transaction rolls back, the claim disappears with it, and the same key can succeed after the wallet exists.

Credit overflow on the destination is also not a durable financial refusal for this key: the service rejects it after locks and before `PENDING`, rolls back, and leaves no idempotency row.

Validation failures (bad JSON, empty ids, non-positive amount, same wallet) never start a transaction when they come through HTTP. The service also rejects non-positive amounts and same-wallet pairs before `BEGIN`, so a non-HTTP caller cannot skip those rules and consume a key. Transient database errors before commit likewise roll back and leave the key retryable.

### Triggers we kept

We asked which triggers the books actually need, removed the rest, and put the two that remained in `001_init.sql`. A later `002_wallet_triggers.sql` was a workaround for a local database that had already recorded an older `001`. The migrator does not re-run a recorded file. `002` was deleted. One migration is the schema. A database that applied the older file has to be reset so `001` runs cleanly.

**`opening_balance` is immutable.** `BEFORE UPDATE` on `wallets` raises if `opening_balance` changes. Debit and credit update `balance` and `updated_at` only, so this trigger does not fire on the transfer path. `mapWalletWriteError` still maps that specific check failure (`23514`, message `opening_balance is immutable`) to a typed error. It is a safeguard for a future write that includes the column, not a step in debit or credit.

**Balance matches the ledger at commit.** Two deferred constraint triggers call the same check: after a wallet’s `balance` is inserted or updated, and after a ledger row is inserted. At commit:

```text
balance = opening_balance + Σ(CREDIT) − Σ(DEBIT)
```

Deferred matters because the debit is applied before the ledger rows exist. An immediate check would fail a correct transfer. At commit, both sides are visible. A commit that moved money without the matching ledger rows fails. A wallet created with `opening_balance = balance` and no ledger rows passes, because the net is zero.

The sum uses `entry_type`. A generated `signed_amount` column was drafted and removed. Direction already lives on the row. A second column would have to stay in agreement with it.

`processed_at` was removed for the same kind of reason. It only recorded when status left `PENDING`. Idempotency, balances, and the ledger do not read it. `created_at` and `updated_at` are the timeline.

### What the database does not re-check

An earlier sketch had a deferred trigger on the transfer itself: refuse to commit `PENDING`, require a `PROCESSED` transfer to have exactly one debit on the source and one credit on the destination for the same amount, and require a `FAILED` transfer to have zero ledger rows.

The trigger that shipped enforces the wallet equation, which is the invariant we needed once opening funds sit outside the ledger. The pair shape is enforced in the service before insert:

- `NewLedgerPair` builds a debit and a credit only for a `PROCESSED` transfer
- `assertConsistentLedgerPair` requires those two rows, the right wallets, the same transfer id, and the same positive amount
- a `FAILED` transfer never calls `InsertPair`

`UNIQUE (transfer_id, entry_type)` still forbids two debits or two credits for one transfer. Tests assert the pair on success and zero rows on failure.

The gap this leaves: a committed `PROCESSED` row with no balance change and no ledger rows would not trip the balance trigger, because that trigger runs on wallet and ledger writes, not on transfer status. The service does not commit that path. A direct SQL update could. The books are protected on the application’s write path and on any commit that touches `balance` or `ledger_entries`.

### Read Committed and ordered row locks

`BeginTx` calls `pool.Begin` with no isolation option. PostgreSQL’s default is Read Committed. The service does not request Serializable.

An isolation level decides what one transaction may see of another’s commits. It is not itself a lock. Serializable would watch for conflicts and abort one side with `40001`, and the application would have to retry. The conflict we care about is two debits of one wallet. `SELECT … FOR UPDATE` already gives those transfers a single order.

`FOR UPDATE` is an exclusive row lock held until commit or rollback. Two transactions can `BEGIN` together and both claim different idempotency keys. The lock is taken only at `LockByIDs`. The grant is atomic: one backend gets the row, the other waits on that row’s queue. Starting at the same moment does not let both hold it. There is no window where both read an unlocked balance and both proceed.

The second transfer runs after the first commits, sees the balance that commit left, and the debit is still:

```sql
UPDATE wallets
SET balance = balance - @amount
WHERE id = @id AND balance >= @amount
```

Zero rows means insufficient funds under the lock. `CHECK (balance >= 0)` is the last line if a predicate is ever skipped.

Lock order is `ORDER BY id`. `A→B` and `B→A` both lock the lower id first. They queue. They do not deadlock.

Disjoint pairs (`A→B` and `C→D`) lock different rows and run in parallel. A global lock would have made those wait on each other for no reason.

Serializable would become the right tool if a rule spanned rows this transaction does not lock, for example “this user’s wallets together stay under a cap.” No such rule exists.

### Why the lock lives in Postgres, not in a Go mutex

A mutex serializes goroutines in one process. A global mutex would also serialize `A→B` with `C→D`. A mutex per wallet id is the fair version, and the locks would still have to be taken in id order to avoid deadlock. It still covers only one address space. A second process, a retry from another instance, a test using its own pool, or a SQL session would not see it. The balance update would still be a database round trip, so the mutex does not shorten the transfer. It adds a consistency hole the row lock does not have.

The `Service` value holds a pool and repositories. Money state lives in the database. Goroutines are safe because each transfer’s critical section is a transaction, not shared Go memory.

### Where each rule lives

| Rule | Where it is enforced |
| --- | --- |
| Malformed JSON, trailing junk, float or null amount, missing fields | Handler, before the service. `400`. |
| Empty idempotency key or wallet id | Handler first. Service normalizes again for non-HTTP callers. |
| Amount `> 0`, from and to distinct | Service, before `BEGIN`. Handler also rejects these so the API returns `400` without a database call. |
| Destination credit would overflow `int64` | Service, after both wallets are locked and before debit. Rolls back; key is not consumed. |
| Idempotency claim, hash compare, locks, debit, credit, ledger, status | Service only. |
| SQL execution, `ON CONFLICT`, row locks | Repository. No business branching. |
| `balance >= 0`, amount `> 0`, distinct wallets, response pair, immutable opening balance, balance equation | Database. |

The handler used to map `ErrSameWallet` and `ErrInvalidAmount` after `CreateTransfer`. On the HTTP path those requests never reach the service, so the branch was unreachable. It was removed. The service checks stay, because service tests and any future caller do not pass through the handler. If the early handler checks were removed, that mapping would have to come back so those errors still become `400`.

Repositories used to reject bad amounts, same-wallet transfers, and a malformed ledger pair. Those checks moved to `service/rules.go` and run before the matching SQL. The repository returns rows, a conflict, or a driver error.

### Package layout

The assignment’s layers are handler, service, repository, and domain. They are top-level packages, not under `internal/`. This module is not a library other modules import, so `internal/` was not earning its keep.

`domain` was renamed to `model`: the entities and the state machine. `Money` moved to `helpers` because it is a shared amount type, not a table. API shapes live in `dto`, separate from the service, so HTTP JSON is not the domain struct. `config` loads `.env`. Existing process environment variables win over the file.

`NewLedgerEntry` for a single free-standing row was removed. The only constructor is `NewLedgerPair`, from a transfer that is already `PROCESSED`.

### SQL parameters

Every request-driven query uses `pgx.NamedArgs` (`@id`, `@amount`, …). pgx rewrites names to positional parameters and sends values separately from the SQL text. Wallet ids, amounts, keys, and JSON bodies are never concatenated into the statement. Numbered placeholders would have been equally safe. Names were chosen because the debit and ledger statements repeat values and are easier to read this way. Migration files are executed as trusted SQL from disk. That path is not client input.

## API contract

JSON is camelCase. Amounts are integers. The body is limited to 1 MiB. A second JSON value after the first is rejected. Unknown fields are ignored.

### `POST /transfers`

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

The handler returns `400` and `invalid_request` before the service when the body is not JSON, the key or either wallet id is empty after trim, the two wallet ids are equal, or `amount` is missing, null, a float, a non-integer, or `<= 0`. `amount` is decoded as `json.Number` and rejected when the text contains `.`, `e`, or `E`.

Success is `201`:

```json
{
  "transferId": "6f1c0e4a-1c2b-4d5e-8f90-123456789abc",
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100,
  "status": "PROCESSED"
}
```

Insufficient funds is `422`. The transfer is committed as `FAILED`:

```json
{
  "transferId": "6f1c0e4a-1c2b-4d5e-8f90-123456789abc",
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100,
  "status": "FAILED",
  "failureReason": "insufficient_funds"
}
```

`failureReason` is omitted unless the status is `FAILED`.

Unknown wallet is `404` with `{ "error": "wallet_not_found" }`. The key is not consumed.

Same key, different `fromWalletId`, `toWalletId`, or `amount` is `409` with `{ "error": "idempotency_key_mismatch" }`.

A retry with the same key and body writes the stored status and the stored JSON bytes from the idempotency row. A processed retry stays `201` with the original `transferId`. An insufficient-funds retry stays `422` with the same `transferId` and `failureReason`. The replay is not rewritten as `200`. Postgres `jsonb` may reorder object keys when the body is read back; the semantic fields are what the client must rely on, and successive replays of the same key return the same stored bytes.

A committed claim with no stored response is treated as a broken invariant and returns `500` with `{ "error": "internal" }`. The workflow stores the response before commit, so this is a bug path, not a client outcome.

Credit overflow on the destination (`balance + amount` would exceed `int64`) is rejected after the wallets are locked and before any debit. The transaction rolls back, so the idempotency key is not consumed. HTTP maps it to `400` / `invalid_request`.

### `POST /wallets`

```json
{ "id": "wallet_1", "balance": 1000 }
```

`balance` is required, may be zero, and must not be negative. Empty id is `400`. Success is `201`:

```json
{ "id": "wallet_1", "openingBalance": 1000, "balance": 1000 }
```

A duplicate id is `409`:

```json
{
  "error": "wallet_already_exists",
  "message": "wallet id already exists, choose a different one"
}
```

An attempt that the database rejects as a change to `opening_balance` is `409` with `opening_balance_immutable`. Create itself sets opening and current balance together. It does not write the ledger.

### `GET /wallets/{id}`

`200` with the same wallet shape. The balance is the stored balance, not a sum computed for the request. `404` with `wallet_not_found` when the id is missing. An empty id is `400`.

### `GET /transfers/{id}`

`200` with the transfer object. A non-UUID id is `400` with message `invalid transfer id`. An unknown id is `404` with `transfer_not_found`.

### `GET /health`

`200` with `{ "status": "ok" }`. It does not touch the database. It is a liveness check for the process.

### Status mapping

| Condition | Status | Body | Writes |
| --- | --- | --- | --- |
| Malformed JSON or invalid fields | 400 | `invalid_request` | none |
| Credit would overflow destination `int64` | 400 | `invalid_request` | rolled back |
| Unknown wallet on transfer | 404 | `wallet_not_found` | rolled back |
| Unknown wallet or transfer on GET | 404 | `wallet_not_found` or `transfer_not_found` | none |
| Insufficient funds | 422 | `status: FAILED`, `failureReason: insufficient_funds` | committed |
| Transfer processed | 201 | `status: PROCESSED` | committed |
| Replay of a completed key | stored code | stored body | none |
| Same key, different body | 409 | `idempotency_key_mismatch` | none |
| Duplicate wallet id | 409 | `wallet_already_exists` | none |
| Request context deadline (`HTTP_REQUEST_TIMEOUT`) | 504 | `timeout` | rolled back if not committed |
| Broken invariant or failed commit | 500 | `internal` | rolled back if not committed |

## Side effects of a commit

A processed transfer commits all of these:

- one `transfers` row in `PROCESSED`, with a null `failure_reason`
- source `balance` decreased by `amount`, destination `balance` increased by `amount`
- one `DEBIT` on the source and one `CREDIT` on the destination, each for `amount`
- the idempotency row updated with `201` and that JSON

An insufficient-funds transfer commits:

- one `transfers` row in `FAILED` with `failure_reason = insufficient_funds`
- zero ledger rows and unchanged balances
- the idempotency row updated with `422` and that JSON

No endpoint deletes history. Ledger rows are insert-only. `opening_balance` is never updated by the service.

## Schema

`repository.Migrate` creates `schema_migrations` if needed, applies `*.sql` files from `MIGRATIONS_DIR` (default `migrations`) in name order, and records each filename in the same transaction as the file. Recorded files are not edited or re-applied.

```sql
CREATE TABLE wallets (
    id               text PRIMARY KEY CHECK (length(id) > 0 AND id = btrim(id)),
    opening_balance  bigint NOT NULL CHECK (opening_balance >= 0),
    balance          bigint NOT NULL CHECK (balance >= 0),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE idempotency_records (
    idempotency_key  text PRIMARY KEY CHECK (length(idempotency_key) > 0 AND idempotency_key = btrim(idempotency_key)),
    request_hash     text NOT NULL,
    request_body     jsonb NOT NULL,
    response_code    integer,
    response_body    jsonb,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT idempotency_response_pair CHECK (
        (response_code IS NULL AND response_body IS NULL)
        OR (response_code IS NOT NULL AND response_body IS NOT NULL)
    )
);

CREATE TABLE transfers (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key  text NOT NULL UNIQUE REFERENCES idempotency_records (idempotency_key),
    from_wallet_id   text NOT NULL REFERENCES wallets (id),
    to_wallet_id     text NOT NULL REFERENCES wallets (id),
    amount           bigint NOT NULL CHECK (amount > 0),
    status           text NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason   text,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT transfers_distinct_wallets CHECK (from_wallet_id <> to_wallet_id),
    CONSTRAINT transfers_failure_reason CHECK (
        (status = 'FAILED' AND failure_reason IS NOT NULL AND length(failure_reason) > 0)
        OR (status <> 'FAILED' AND failure_reason IS NULL)
    )
);

CREATE TABLE ledger_entries (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    wallet_id    text NOT NULL REFERENCES wallets (id),
    transfer_id  uuid NOT NULL REFERENCES transfers (id),
    entry_type   text NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount       bigint NOT NULL CHECK (amount > 0),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (transfer_id, entry_type)
);

CREATE INDEX transfers_from_wallet_id_idx ON transfers (from_wallet_id);
CREATE INDEX transfers_to_wallet_id_idx ON transfers (to_wallet_id);
CREATE INDEX ledger_entries_wallet_id_idx ON ledger_entries (wallet_id);
```

The application generates the transfer UUID with `google/uuid` and inserts it. The column default exists so a raw insert still gets an id. Wallet and key checks require a non-empty trimmed value, which matches `helpers.NormalizeID` and `NormalizeIdempotencyKey`.

Indexes on `transfers.from_wallet_id`, `transfers.to_wallet_id`, and `ledger_entries.wallet_id` serve the foreign-key lookups and the per-wallet ledger sum. There is no transfer-history endpoint.

Insert order inside a transfer is claim, then transfer (the transfer references the claim), then balance updates and ledger rows. There is no circular foreign key.

## Transfer workflow

The handler decodes and applies the HTTP checks above. `CreateTransfer` is the business boundary. Repositories use the transaction stored on the context by `BeginTx`. They do not choose status codes.

```mermaid
sequenceDiagram
  participant Handler
  participant Service
  participant DB
  Handler->>Service: CreateTransfer
  Service->>Service: Validate amount and distinct wallets, hash body
  Service->>DB: BEGIN
  Service->>DB: Insert idempotency key ON CONFLICT DO NOTHING
  alt Key already owned
    Service->>DB: Load stored hash and response
    Service->>DB: ROLLBACK
    Service-->>Handler: Stored response, or 409
  else Key claimed
    Service->>DB: Lock wallets ORDER BY id FOR UPDATE
    alt Fewer than two wallets
      Service->>DB: ROLLBACK
      Service-->>Handler: 404
    else Destination credit would overflow int64
      Service->>DB: ROLLBACK
      Service-->>Handler: 400
    else Both locked
      Service->>DB: Insert PENDING transfer
      alt Locked balance cannot cover amount
        Service->>DB: Mark FAILED, store 422, COMMIT
        Service-->>Handler: 422
      else Funds available
        Service->>DB: Conditional debit, credit, ledger pair, PROCESSED
        Service->>Service: Assert balance equation
        Service->>DB: Store 201 and COMMIT
        Service-->>Handler: 201
      end
    end
  end
```

`prepareTransferRequest` normalizes ids, rejects a non-positive amount and a same-wallet pair, and builds the claim. That happens before `BEGIN`.

Claim:

```sql
INSERT INTO idempotency_records (idempotency_key, request_hash, request_body, created_at, updated_at)
VALUES (@idempotency_key, @request_hash, @request_body::jsonb, @created_at, @updated_at)
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING idempotency_key;
```

No returned row means the key exists. The service loads it, rolls the transaction back, and returns. The rollback is not undoing an insert. The conflict inserted nothing. It closes the transaction that was opened for the claim so a duplicate cannot commit. `defer tx.Rollback` would also abort on return. The explicit rollback makes that path obvious.

Canonical payload:

```text
{"amount":100,"fromWalletId":"wallet_1","toWalletId":"wallet_2"}
```

After a successful claim both wallets are locked:

```sql
SELECT id, opening_balance, balance, created_at, updated_at
FROM wallets
WHERE id = ANY(@ids)
ORDER BY id
FOR UPDATE;
```

Fewer than two rows is an unknown wallet. The deferred rollback releases the claim. The `PENDING` insert has not run yet.

When both rows are held, the service checks `CanCredit` on the locked destination before inserting `PENDING`. An `int64` overflow on credit rolls back immediately so the key is not consumed and no debit can leave a partial move.

Otherwise it inserts the transfer as `PENDING`, then `CanDebit` on the locked source balance. If the locked balance is short, it does not run the debit statement. It marks the transfer `FAILED`, stores `422`, and commits — terminal business failure, key stays consumed.

If the locked balance covers the amount, the conditional debit runs. Under the lock it should update one row. Zero rows is treated as insufficient funds as well, so a stale in-memory check cannot overdraw. The credit, `MarkProcessed`, the ledger pair, and `UpdateStatus` follow. `assertBalancesMatchLedger` reads the wallets and `SumSigned` and refuses to store the response if `balance` disagrees with `opening_balance + net`. The deferred database trigger checks the same equation again at commit.

Wallet creation is its own transaction: insert `id`, `opening_balance`, and `balance` as the same value. `ON CONFLICT DO NOTHING` with zero rows affected is `409`. It does not use the idempotency table.

## Idempotency and retries

Exactly-once means exactly-once at the API for a given key and body. The database runs that body once. A retry reads the recorded result.

| Delivery | Database | Client |
| --- | --- | --- |
| First request, commits | One claim, one transfer, at most one ledger pair | `201` or `422` |
| Retry after the response was lost | Insert conflicts, read the stored response, roll back | Same code, body, and `transferId` |
| Retry while the first transaction is open | Blocks on the unique index | Unblocks into the stored row once the first commits, or becomes the claim if the first rolls back |
| Unknown wallet or crash before commit | Claim disappears with the rollback | The retry is a new first attempt |
| Same key, different body, after a commit | Stored row unchanged | `409` |
| Process restart after commit | Row is durable | Replay still returns it |
| Process restart before commit | Nothing committed | Retry runs the transfer once |

Under Read Committed, `INSERT … ON CONFLICT DO NOTHING` waits if another transaction has inserted the same key and not yet committed. After that commit, the waiting insert conflicts and inserts nothing. After a rollback, the waiting insert becomes the claim. Two in-flight duplicates cannot both move money. The service does not take `FOR UPDATE` on the idempotency row for the duplicate path. It only reads and rolls back.

Keys are not expired. One key means one outcome for the life of the database. There is no authentication, so the key is global. A collision between unrelated clients is either a replay or a `409`.

## Concurrency and consistency

| Scenario | What happens |
| --- | --- |
| Same wallet, different keys | `FOR UPDATE` serializes the debits. The later one sees the committed balance. |
| Disjoint wallet pairs | Different row locks. They proceed in parallel. |
| `A→B` concurrent with `B→A` | Same lock order. They finish. They do not deadlock. |
| Same idempotency key | Unique claim. One transfer, one debit. The other callers receive that result. |
| Reads during writes | Read Committed. A reader sees the last commit. `CHECK (balance >= 0)` and the lock mean that commit is not a negative balance. |

Ten transfers of 20 against a balance of 100 produce five `PROCESSED` and five `FAILED` outcomes. Which five win is whichever five take the lock first. The balance ends at 0 and the ledger debits sum to 100. Tests assert the counts and the sums.

The in-memory `CanDebit` runs only after the row lock is held. It is not a check-then-act on an unlocked read. The conditional `UPDATE` is the second guard. The check constraint is the third. The deferred trigger is the fourth, for the equation rather than for the lock.

## Failure modes

| Failure | Boundary | Client sees | Retry |
| --- | --- | --- | --- |
| Invalid JSON, empty key, non-positive amount, same wallet | Handler, and the service if called directly | `400` on HTTP. Service returns the model error. | Fix the body. No key was stored. |
| Wallet id does not exist | Transaction rolled back | `404` | Same key works after the wallet exists. |
| Destination credit would overflow `int64` | Transaction rolled back after locks, before debit | `400` | Same key can be retried with a smaller amount or different destination. |
| Balance below amount | Committed as `FAILED` | `422` | Stays `422` even if funds arrive later. A new key is required to try again. |
| Duplicate key, same body | Read stored response, roll back | Original `201` or `422` | Already the retry. |
| Duplicate key, different body | Roll back, no new transfer | `409` | Only the original body replays. |
| Crash or lost response before commit | Database rolls back | Error, or nothing | Retry is the first real attempt. |
| Crash after commit, response lost | Rows are durable | Error, or nothing | Retry returns the stored result and does not debit again. |
| Concurrent debits that overdraw | Row locks plus conditional update | Some `201`, some `422` | Each key keeps the outcome it committed. |
| Opposite directions | Ordered row locks | Both finish, success or insufficient funds | No deadlock handling on the client. |
| Request timeout | Context canceled; open tx rolls back | `504` | Safe to retry with the same key. |
| Database unavailable | No commit | `500` | Safe once the database is back. A key is consumed only by a commit. |
| Status change other than `PENDING → PROCESSED` or `PENDING → FAILED` | Rejected in memory | `500` if it reaches the handler | A bug, not a client retry. |

Partial work inside the transaction is not a client-visible state. Balances, ledger rows, the transfer status, and the idempotency response commit together. The deferred trigger is what makes a commit that changed a balance include a matching ledger.

## State machine

```text
PENDING → PROCESSED
PENDING → FAILED
```

`NewTransfer` starts at `PENDING`. `MarkProcessed` and `MarkFailed` reject every other transition, including a second call on a terminal transfer, and they leave the status unchanged when they reject. `MarkFailed` requires a non-empty reason. The only reason this version writes is `insufficient_funds`. The column is text so another reason can be added without a schema change. The check constraint requires a non-empty reason for `FAILED` and a null reason otherwise. It does not enumerate the reason strings.

`GET /transfers/{id}` returns whatever is committed. The service updates the row to a terminal status before commit, so the rows this API creates are `PROCESSED` or `FAILED`.

## Layout and configuration

Dependencies are `jackc/pgx/v5`, `google/uuid`, and `joho/godotenv`. No ORM.

- `cmd/server/main.go` loads config, opens a sized pgx pool, migrates, and serves HTTP with server and per-request timeouts. It shuts down on interrupt with a five-second timeout.
- `model` holds wallet, transfer, ledger entry, and idempotency record, including the status transition and the canonical hash.
- `helpers` holds `Money`, id normalization, and the positive-amount and non-negative-balance constructors.
- `service` begins every transaction and runs the workflow. `rules.go` is the guard set called before repository writes.
- `repository` executes SQL. `QuerierFrom` uses the transaction on the context, or the pool when there is none.
- `handler` decodes JSON, maps the errors in the status table, and writes the body. A replay is written from the stored bytes.
- `dto` is the JSON view of a wallet or transfer.

`DATABASE_URL` is required. `HTTP_ADDR` defaults to `:8080`. `MIGRATIONS_DIR` defaults to `migrations`. `DB_POOL_MAX_CONNS` defaults to `20` and `DB_POOL_MIN_CONNS` to `2` (set explicitly so acquire wait is not left to pgx’s CPU-based default). HTTP server timeouts default to read-header `5s`, read `10s`, write `65s`, idle `60s`. `HTTP_REQUEST_TIMEOUT` defaults to `60s` and cancels the request context so pool acquire and DB work cannot wait unbounded; handlers map that to `504` / `timeout`. Tests use `TEST_DATABASE_URL`, defaulting to `postgres://wallet:wallet@localhost:5433/wallet_test?sslmode=disable` (never `DATABASE_URL`, so test `TRUNCATE` cannot wipe hand-used API data). `docker-compose.yml` publishes Postgres 16 on **5433** and creates `wallet_test` on first volume init.

The process logs listen and fatal errors with the standard library logger. There is no metrics endpoint and no per-transfer structured log line. The durable record of an attempt is the idempotency row and the transfer row.

## Test plan

Domain, helper, rules, and handler tests do not need a database. Handler tests use a stub service and prove HTTP mapping only. Service and repository tests run against PostgreSQL. They fail hard if the database cannot be reached, unless `SKIP_DB_TESTS=1` is set explicitly. Default DSN resolution is `TEST_DATABASE_URL`, then `postgres://wallet:wallet@localhost:5433/wallet_test?sslmode=disable`. Each case was aimed at a behavior, then the code that makes that behavior true.

**Money and model**

- `NewAmount` rejects zero and negative values. `NewBalance` allows zero and rejects negatives.
- A new transfer is `PENDING`. `MarkProcessed` and `MarkFailed` succeed once. A second transition fails and leaves the status. An empty failure reason is rejected.
- `NewLedgerPair` requires a `PROCESSED` transfer and builds one debit on the source and one credit on the destination.
- The canonical hash is stable for the same `amount`, `fromWalletId`, and `toWalletId`, and differs when any of those change. Model `StoreResponse` fills code and body once and rejects a second store.

**Service rules, no database**

- Positive amount, distinct wallets, and the ledger-pair shape that used to sit in the repositories are rejected in `rules.go` with the same model errors.

**Handler**

- Missing fields, empty key, `amount <= 0`, floats, and `fromWalletId == toWalletId` return `400` and `invalid_request`.
- Service errors map to the status table (`404`, `409`, `400` for overflow, `500`, `504` for timeout).
- Fresh `201` and replay paths write the outcome status and body (including raw replay bytes).
- `GET /health` returns `200` and `{"status":"ok"}`.

**Repository, against PostgreSQL**

- Wallet create, get, and duplicate id.
- Debit, credit, and `FOR UPDATE` locking. Debit of more than the balance updates zero rows.
- Updating `opening_balance` raises, and the error maps to the immutable-balance error.
- A processed transfer in one transaction has one debit and one credit. A failed transfer has no ledger rows and unchanged balances.
- Claiming a key twice does not insert a second row. Different bodies hash differently.
- A claimed key that is rolled back is gone, and the same key can be claimed again.
- Repository `StoreResponse` updates only when `response_code` is still null; a second write returns `ErrConflict` and leaves the original body and code.

**Service, against PostgreSQL**

- Duplicate wallet id returns `ErrWalletAlreadyExists` and does not replace the original balance.
- Happy path: balances move, `opening_balance` stays at the created value, exactly one debit and one credit match the amount and the wallets, and a replay returns `201` without moving money or adding ledger rows.
- After a `PROCESSED` commit, the same key with a different amount returns `ErrIdempotencyMismatch` and does not debit again.
- Insufficient funds: `422`, `FAILED`, zero ledger rows. The same key with a different amount returns `ErrIdempotencyMismatch`.
- Failed replay with the same body returns `422`, the original `transferId` / failure fields, and successive replays share the same stored response bytes. Balances stay unchanged.
- Sticky failure: after a `422`, another transfer funds the source; the same key still replays `422` with the original `transferId` and does not move money. A **new** key with the same amount then succeeds.
- Unknown wallet rolls back the claim; creating the wallet and retrying the same key succeeds.
- Credit overflow rolls back with `ErrBalanceOverflow`; no idempotency row and no transfer remain.
- Zero, negative, and same-wallet calls return the model errors and do not need a successful transfer.

**Concurrency, against PostgreSQL**

Concurrency tests live in `service/` against a real pool. Run them with `-race` when checking for data races (`go test ./service/ -race`).

- Two transfers of 40 from a wallet with 50: one `201`, one `422`, shared balance 10, one ledger pair, equation holds.
- Ten transfers of 20 from a wallet with 100: five processed, five failed, source 0, destination 100, ledger nets −100 and +100.
- Ten callers, one key: one transfer, one ledger pair, every success shares that `transferId`.
- Concurrent `A→B` and `B→A` finish within 10 seconds, balances stay non-negative, and the two balances still sum to the openings.
- Eight disjoint pairs of 25 all return `201` and reconcile. They are the case that must not serialize on one global lock.
- Mixed contended writes, opposite directions, disjoint pairs, and concurrent reads finish, never go negative, and still satisfy the business rules and the ledger equation.

There is no separate HTTP-layer concurrency test; parallel safety is proven at the service/database boundary that holds the money locks.

## Tradeoffs that remain

**No durable `PENDING` and no recovery worker.** A crash drops the in-flight attempt. That is correct while every side effect is inside this transaction. A later call to an external system would need a durable in-flight state and a worker. The status column is ready for that. The commit path is not.

**The database enforces the balance equation, and the service enforces the pair.** A hand-written `UPDATE` of a transfer’s status does not by itself prove the ledger pair. Any commit that changes `balance` or inserts a ledger row still has to satisfy `balance = opening_balance + credits − debits`. Reconciliation tests cover the application path.

**Insufficient funds is sticky. An unknown wallet is not.** Those are opposite choices about the same key, on purpose. A top-up after `422` needs a new key (`TestCreateTransfer_FailedStickyAfterFundsArrive`). A wallet created after `404` may reuse the key. Credit overflow and other pre-commit errors behave like the unknown-wallet path: the claim disappears with the rollback.

**Keys do not expire, and they are global.** There is no account or API key to scope them. Retention and per-caller namespaces are out of scope.

**One currency, no history API, no metrics.** The transactional path is what the tests cover. A ledger list and dashboards were left out so that path stayed small enough to race.

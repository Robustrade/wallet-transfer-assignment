# Evaluate code against guides



> I need you to do two different evaluations of the code, based on

> 1) [evaluation_guide.md](evaluation_guide.md)

> 2) [copilot-instructions.md](.github/copilot-instructions.md)



<details><summary>17 previous messages</summary>



> I’ll read the two evaluation documents first, then inspect the repo against each lens separately so the feedback doesn’t get blended into one mush.

>

> <details><summary>Explored 2 files, 1 list</summary>

>

> - Read `./evaluation_guide.md`

> - Read `./.github/copilot-instructions.md`

> - Listed files

>

> </details>

>

> I’ve got the rubric. Now I’m going through the schema, service, repos, handlers, and tests with an eye toward transfer correctness and idempotency first, then the code-shape expectations from the Copilot instructions.

>

> <details><summary>Explored 4 files</summary>

>

> - Read `./internal/service/transfer_service.go`

> - Read `./migrations/001_init.sql`

> - Read `./internal/repository/wallet_repo.go`

> - Read `./internal/repository/idempotency_repo.go`

>

> </details>

>

> The core transaction path is fairly disciplined: it claims idempotency inside the transaction, locks wallets deterministically, and writes both ledger rows before marking the transfer processed. I’m checking whether the database backs those guarantees strongly enough, and whether failure semantics match the assignment’s “exactly-once” bar.

>

> <details><summary>Explored 5 files</summary>

>

> - Read `./internal/repository/transfer_repo.go`

> - Read `./internal/domain/idempotency.go`

> - Read `./internal/repository/ledger_repo.go`

> - Read `./internal/handler/transfer_handler.go`

> - Read `./internal/domain/transfer.go`

>

> </details>

>

> A couple of possible concerns are emerging around schema-level invariants: the code creates balanced ledger pairs, but the table itself does not prevent duplicate or incomplete ledger rows for a transfer. I’m reading the tests now to see whether that risk is covered behaviorally.

>

> <details><summary>Explored 5 files</summary>

>

> - Read `./tests/integration/transfer_test.go`

> - Read `./internal/service/transfer_service_test.go`

> - Read `./ASSIGNMENT.md`

> - Read `./tests/integration/concurrency_test.go`

> - Read `./README.md`

>

> </details>

>

> The tests do cover happy path, duplicate replay, mismatched idempotency, insufficient funds, and several concurrent cases. I’m going to grab line references and run the test suite so the recommendation is grounded in both code review and actual verification.

>

> <details><summary>Explored 4 files, ran 1 command</summary>

>

> <details><summary>Explored 4 files</summary>

>

> - Read `./internal/repository/ledger_repo.go`

> - Read `./internal/service/transfer_service.go`

> - Read `./README.md`

> - Read `./migrations/001_init.sql`

>

> </details>

>

> - Ran `go test ./...`

>

> </details>

>

> `go test ./...` can’t compile in the current shell because the local Go install has a stdlib/toolchain mismatch (`go1.21.5` package objects with `go1.26.4` tool). That’s an environment problem rather than a code failure, so I’m noting it separately and checking commit/development signals next.

>

> <details><summary>Explored 3 files, ran 2 commands</summary>

>

> <details><summary>Explored 3 files</summary>

>

> - Read `./internal/service/interfaces.go`

> - Read `./go.mod`

> - Read `./internal/handler/errors.go`

>

> </details>

>

> - Ran `git status --short`

>

> - Ran `git log --oneline --decorate --max-count=12`

>

> </details>

>

> The working tree is not a committed candidate yet: most implementation files are untracked and README is modified. For the development-practices part of the evaluation, that matters because commit quality can’t really be assessed from the assignment implementation itself.

>

> <details><summary>Explored 5 files</summary>

>

> - Read `./docker-compose.test.yml`

> - Read `./Makefile`

> - Read `./tests/integration/main_test.go`

> - Read `./internal/service/transfer_service_test.go`

> - Read `./tests/integration/concurrency_test.go`

>

> </details>

>

> There’s a Dockerized test path using `golang:1.24-alpine`, which matches the module’s toolchain better than this host shell. I’m going to try that for a cleaner verification signal.

>

> <details><summary>Ran 1 command</summary>

>

> - Ran `make test-docker`

>

> </details>

>

> Ignore code commits for now

</details>



I ignored commit quality, per your note.



**Evaluation 1: `evaluation_guide.md`**



Overall recommendation: **Hire**, leaning strong technically, with a few important follow-ups.



Strengths:

- Transaction flow is solid: `CreateTransfer` validates, begins a transaction, claims idempotency, locks wallets, updates balances, writes ledger entries, updates status, and commits atomically in [internal/service/transfer_service.go](./internal/service/transfer_service.go:66).

- Concurrency strategy is well chosen: `SELECT FOR UPDATE` with deterministic wallet ordering in [internal/service/transfer_service.go](./internal/service/transfer_service.go:242) addresses same-wallet debits and bidirectional deadlocks.

- Idempotency is durable and backed by a primary key in [migrations/001_init.sql](./migrations/001_init.sql:27).

- Tests include meaningful integration coverage for duplicate requests, mismatched idempotency keys, insufficient funds, ledger correctness, same-wallet concurrency, bidirectional transfers, and concurrent same-key idempotency.

- Dockerized test suite passes: `make test-docker`.



Risks:

- Ledger correctness is mostly enforced by service code, not the database. The schema allows more than one `DEBIT` or `CREDIT` per transfer because [ledger_entries](./migrations/001_init.sql:62) has no uniqueness like `(transfer_id, entry_type)`. The app creates a pair in [ledger_repo.go](./internal/repository/ledger_repo.go:20), but DB constraints do not guarantee “exactly two entries.”

- Failure idempotency is not truly cached. On insufficient funds or wallet-not-found, the transaction rolls back and frees the key in [transfer_service.go](./internal/service/transfer_service.go:118). That means the same key can later succeed after conditions change, rather than returning the original failed result. `MarkFailed` exists but appears unused.

- Documentation overclaims a bit: README says `idempotency_records.transfer_id` is an FK and includes `response_code` / `response_body`, but the migration does not in [migrations/001_init.sql](./migrations/001_init.sql:27).



Category notes:

- Database schema: good foundation, but ledger invariants and idempotency `transfer_id` referential integrity are weaker than ideal.

- Transaction/locking: strong.

- Idempotency: strong for successful transfers, weaker for failed-result replay.

- Layering/code quality: clean and understandable.

- Testing: good integration coverage; service unit tests are lighter than ideal for the main transfer workflow.

- Development practices: not evaluated, per your instruction.



**Evaluation 2: `.github/copilot-instructions.md`**



Findings:

- **Medium:** Ledger double-entry correctness is not database-enforced. Add a unique constraint on `(transfer_id, entry_type)` at minimum, and consider constraints/triggers or reconciliation checks for exactly one debit and one credit per processed transfer.

- **Medium:** Exactly-once API semantics are incomplete for failed requests. If the assignment expects “same key returns original result” for failures too, persist failed idempotency outcomes instead of rolling back the key on business failures.

- **Low:** README/schema mismatch may mislead reviewers about guarantees that are not actually implemented.

- **Low:** Tests are strong at integration level, but unit/service tests do not exercise the happy-path transfer orchestration with mocked transaction/idempotency/ledger behavior.



Copilot-style verdict: correctness-first implementation with good transaction boundaries and concurrency handling. I’d ask follow-up interview questions around why failed idempotency keys are reusable, and how they would make ledger invariants auditable and enforceable at the database layer.



> Can you do another review now please



<details><summary>7 previous messages</summary>



> I’ll do a fresh pass from the current workspace state, treating this as a review rather than carrying over the previous verdict. I’ll check what changed, then re-read the high-risk transfer/idempotency/schema paths.

>

> <details><summary>Explored 1 search, ran 2 commands</summary>

>

> <details><summary>Explored 1 search</summary>

>

> - Searched for `MarkFailed|FAILED|response_body|response_code|UNIQUE|ledger_entries|idempotency` in `.`

>

> </details>

>

> - Ran `git diff -- migrations/001_init.sql internal/service/transfer_service.go internal/repository/ledger_repo.go internal/repository/idempotency_repo.go README.md`

>

> - Ran `git status --short`

>

> </details>

>

> I see there have been targeted changes since the earlier pass: ledger now has a uniqueness constraint, and the service has new failed-transfer/idempotency handling. I’m reading those paths closely because they can fix one issue while changing retry semantics.

>

> <details><summary>Explored 4 files</summary>

>

> - Read `./migrations/001_init.sql`

> - Read `./internal/service/transfer_service.go`

> - Read `./tests/integration/transfer_test.go`

> - Read `./internal/repository/idempotency_repo.go`

>

> </details>

>

> The new failed-idempotency test covers insufficient funds replay, which is good. One thing I’m checking now is whether failure persistence can silently commit a partial state if creating the failed transfer or marking idempotency fails, because those errors are currently logged/ignored in a couple places.

>

> <details><summary>Explored 1 file, ran 2 commands</summary>

>

> <details><summary>Explored 1 file</summary>

>

> - Read `./internal/service/transfer_service.go`

>

> </details>

>

> - Ran `gofmt -l internal tests cmd`

>

> - Ran `make test-docker`

>

> </details>

>

> The test suite passes in Docker, and `gofmt -l` reports one file still needs formatting: `internal/service/transfer_service_test.go`. I’ve got enough for a review now; the main remaining comments are about error handling in the new failed-idempotency path and clarifying ledger semantics for failed transfers.

</details>



**Findings**



- **[P1] Don’t commit failed-idempotency state after ignoring persistence errors**

  In [internal/service/transfer_service.go](./internal/service/transfer_service.go:116) and [internal/service/transfer_service.go](./internal/service/transfer_service.go:131), failures while marking idempotency as `FAILED` or creating the failed transfer are logged/ignored, then the transaction is committed. If either write fails, this can persist an `IN_PROGRESS` idempotency record with no usable transfer, causing future retries to return `IDEMPOTENCY_KEY_IN_PROGRESS` indefinitely. For exactly-once semantics, these writes should be treated as part of the atomic outcome: return/rollback on failure rather than commit partial idempotency state.



- **[P2] Failed transfer rows weaken the “every transfer has two ledger entries” invariant unless explicitly scoped**

  `recordFailedTransfer` creates a `FAILED` row in [internal/service/transfer_service.go](./internal/service/transfer_service.go:331), but no ledger entries are created. That is reasonable financially for a failed movement, but it conflicts with the assignment/README wording that every transfer generates exactly two ledger entries. I’d either document/enforce “every `PROCESSED` transfer has one debit and one credit” or avoid creating transfer rows for pre-ledger business failures and cache the error outcome separately.



- **[P3] Formatting check fails**

  `gofmt -l internal tests cmd` reports [internal/service/transfer_service_test.go](./internal/service/transfer_service_test.go:97). Run `gofmt -w internal/service/transfer_service_test.go`.



**What Improved**



The ledger schema now has `CONSTRAINT unique_transfer_entry_type UNIQUE (transfer_id, entry_type)` in [migrations/001_init.sql](./migrations/001_init.sql:69), which closes the earlier duplicate debit/credit gap. The new failed idempotency replay test in [tests/integration/transfer_test.go](./tests/integration/transfer_test.go:255) is also a good addition.



**Verification**



`make test-docker` passes. The local Docker cleanup still reports the compose network is in use because of existing orphan containers, but the test runner exits successfully.



> Have you check both [evaluation_guide.md](evaluation_guide.md) and [copilot-instructions.md](.github/copilot-instructions.md)



Yes. I used both:



- [evaluation_guide.md](./evaluation_guide.md): broader candidate-evaluation rubric, including schema, locking, idempotency, layering, testing, and overall recommendation.

- [.github/copilot-instructions.md](./.github/copilot-instructions.md): PR-review lens focused on actionable correctness issues: transaction safety, exactly-once semantics, concurrency, ledger consistency, layering, and tests.



My latest review was primarily written in the Copilot/actionable PR-review style, but the issues map directly to the evaluation guide too. The current overall recommendation would still be **Hire**, with the main follow-up being: make failed-idempotency persistence atomic instead of logging and committing after ignored errors.



> Just do a fresh review at your end, and let me know your opinon



<details><summary>4 previous messages</summary>



> I’ll do one clean final pass now, using the two review guides as the frame and ignoring commit history as requested. I’ll re-check the exact code paths I’d base a hiring/review opinion on, then give you the short version.

>

> <details><summary>Explored 4 files, ran 1 command</summary>

>

> <details><summary>Explored 4 files</summary>

>

> - Read `./.github/copilot-instructions.md`

> - Read `./evaluation_guide.md`

> - Read `./internal/service/transfer_service.go`

> - Read `./migrations/001_init.sql`

>

> </details>

>

> - Ran `gofmt -l internal tests cmd`

>

> </details>

>

> The current code has fixed the earlier “logged and ignored persistence errors” concern: failed idempotency writes now return errors and roll back instead of committing partial state. Formatting is clean too, so I’m doing one final Docker test run for a real verification signal.

>

> <details><summary>Ran 1 command</summary>

>

> - Ran `make test-docker`

>

> </details>

</details>



**Findings**



- **[P3] Double-entry is not fully database-enforced.**

  The new `(transfer_id, entry_type)` uniqueness constraint is a good improvement, and the service writes debit/credit in one transaction. But the DB still cannot independently guarantee that every `PROCESSED` transfer has exactly one debit and one credit. I would accept this for the assignment, but in production I’d want a reconciliation check, trigger, or stronger invariant around processed transfers.



- **[P3] Clarify failed-transfer ledger semantics.**

  Failed transfers, such as insufficient funds, are now persisted as `FAILED` without ledger entries. That is financially correct, but the docs/rubric language says “every transfer” gets two ledger rows. I’d phrase it as “every `PROCESSED` transfer” to avoid a false invariant.



**Opinion**



This is a **Hire** for me, leaning strong. The core money-movement path is thoughtfully built: explicit transaction, idempotency key claim, deterministic `SELECT FOR UPDATE` wallet locking, safe balance updates, ledger pair creation, and replay handling. The recent fixes also improved failed idempotency handling: persistence errors now roll back instead of leaving a stuck `IN_PROGRESS` record.



Testing is above average for this kind of assignment: duplicate replay, mismatch, insufficient funds, ledger checks, same-wallet concurrency, bidirectional no-deadlock, and concurrent same-key idempotency are covered.



Verification: `make test-docker` passes, and `gofmt -l internal tests cmd` is clean. The only test-run noise is Docker reporting an existing orphan/network in use, not a test failure.



> Should we prefer java or golang in a project like this



For a wallet transfer service like this, I’d slightly prefer **Go** unless the company already has a strong Java/Spring ecosystem.



Go is a very good fit here because:

- Simple concurrency model and low runtime overhead.

- Easy to write small, explicit services with clear transaction boundaries.

- Fast builds, simple deployment, small containers.

- Less framework magic, which helps in correctness-heavy systems like transfers.



Java is also a strong choice, especially if:

- The org already uses Spring Boot, JPA/Jooq, Kafka, observability, and mature internal tooling.

- You need a large hiring pool and lots of enterprise integrations.

- The system is part of a bigger JVM platform with shared libraries and governance.



My practical take:

**Greenfield, small-to-medium financial service:** Go.

**Large enterprise platform with existing JVM infrastructure:** Java.

**For this assignment specifically:** Go is totally appropriate, and maybe even clearer because the transaction/idempotency logic is easy to see without much framework ceremony.



> what were you saving about the go version mismatch in this case



I was saying the **host machine’s Go install looked broken/mismatched**, not that the project itself had a Go version problem.



When I ran:



```bash

go test ./...

```



locally, it failed with errors like:



```text

compile: version "go1.21.5" does not match go tool version "go1.26.4"

```



That means the local Go toolchain was using compiled standard-library/package artifacts from one Go version (`1.21.5`) while the active Go tool binary was another version (`1.26.4`). That is usually an environment/cache/toolchain issue.



The project itself declares:



```go

go 1.21

toolchain go1.24.0

```



and the Docker test runner uses Go `1.24`, so I verified with:



```bash

make test-docker

```



That passed. So my conclusion was: **ignore the local `go test ./...` failure; Docker gives the reliable signal here.**



> I never worked in Go, but I have a interview in which I might be asked to extend upon this repo. It means I might need to add features in the current repo. Consider I will not be using AI, how can I do that, can you help me understand the codebase, with specifically nuances that exists in Go, and also how the current repo works



<details><summary>2 previous messages</summary>



> Absolutely. I’ll give you a practical “how to survive and extend this repo in an interview” map: where the code lives, how one transfer flows through it, and the Go-specific things that may feel odd coming from other languages. I’ll quickly inspect the remaining entrypoint/handler/repository plumbing so I don’t hand-wave the architecture.

>

> <details><summary>Explored 7 files</summary>

>

> - Read `./cmd/server/main.go`

> - Read `./internal/handler/wallet_handler.go`

> - Read `./internal/handler/response.go`

> - Read `./internal/repository/postgres.go`

> - Read `./internal/domain/errors.go`

> - Read `./internal/domain/wallet.go`

> - Read `./internal/domain/ledger_entry.go`

>

> </details>

</details>



Yes, I can help. Here’s the mental model I’d use if you had to extend this repo in an interview without AI.



**Big Picture**



This is a classic layered Go backend:



- `cmd/server/main.go`: app startup, dependency wiring, routes, middleware.

- `internal/domain`: pure data types, validation, business errors.

- `internal/handler`: HTTP parsing and response mapping.

- `internal/service`: business workflow and transaction orchestration.

- `internal/repository`: SQL/database access.

- `migrations`: PostgreSQL schema.

- `tests/integration`: end-to-end HTTP + real Postgres tests.



The most important file is [internal/service/transfer_service.go](./internal/service/transfer_service.go:66). That is where the money movement actually happens.



**How A Transfer Works**



For `POST /transfers`:



1. Route is registered in [cmd/server/main.go](./cmd/server/main.go:66).

2. Handler parses JSON in [internal/handler/transfer_handler.go](./internal/handler/transfer_handler.go:28).

3. Handler calls `service.CreateTransfer`.

4. Service validates request.

5. Service starts a DB transaction.

6. Service claims the idempotency key.

7. Service locks both wallets using `SELECT ... FOR UPDATE`.

8. Service checks sufficient funds.

9. Service creates transfer row.

10. Service updates wallet balances.

11. Service creates debit/credit ledger rows.

12. Service marks transfer `PROCESSED`.

13. Service marks idempotency record `COMPLETED`.

14. Transaction commits.



That sequence is the heart of the assignment.



**Go Nuances To Know**



Go uses packages, not classes. Files in the same folder with the same `package` share scope. For example, all files in `internal/service` are part of package `service`.



Capitalized names are exported:



```go

func NewTransferService(...)

type TransferService struct { ... }

```



lowercase names are private to the package:



```go

func (s *TransferService) lockWalletsOrdered(...)

```



Methods look like this:



```go

func (s *TransferService) CreateTransfer(...)

```



The `(s *TransferService)` part is the receiver. It is roughly like `this` / `self`.



Errors are explicit. Go does not use exceptions:



```go

wallet, err := repo.GetByID(...)

if err != nil {

    return nil, err

}

```



This is everywhere. In an interview, don’t ignore errors unless there is a very deliberate reason.



`context.Context` is passed through most calls:



```go

CreateTransfer(ctx context.Context, req ...)

```



It carries cancellation, timeout, request lifetime, etc. HTTP handlers use `r.Context()`.



`defer` runs when the function returns:



```go

defer tx.Rollback(ctx)

```



This is a safety net. If the transaction commits first, the later rollback is harmless.



Interfaces are implicit. A type satisfies an interface if it has the required methods. For example [internal/service/interfaces.go](./internal/service/interfaces.go:9) defines what the service needs from repositories. The repository structs don’t say “implements WalletRepo”; they just have the right methods.



Struct tags control JSON:



```go

ID string `json:"id"`

```



So API JSON uses `id`, while Go code uses `ID`.



Pointers matter. `*string` means nullable/optional:



```go

ErrorReason *string `json:"errorReason,omitempty"`

```



That maps naturally to nullable DB fields and optional JSON fields.



**Database Pattern**



The repo uses `pgx`, not an ORM. SQL is explicit.



Important abstraction: [repository.DBTX](./internal/repository/postgres.go:16).



```go

type DBTX interface {

    Exec(...)

    Query(...)

    QueryRow(...)

}

```



Both a connection pool and a transaction can satisfy this. That means repository methods can run either inside or outside a transaction:



```go

walletRepo.GetByID(ctx, s.db.Pool, id)

walletRepo.GetByIDForUpdate(ctx, tx, id)

```



This is a very Go-ish, clean design.



**How To Add A Feature**



Suppose the interviewer asks: “Add an endpoint to list transfers for a wallet.”



You’d likely do this:



1. Add repository method:

   - Interface in [internal/service/interfaces.go](./internal/service/interfaces.go:15)

   - Implementation in `internal/repository/transfer_repo.go`



2. Add service method:

   - `func (s *TransferService) ListTransfersForWallet(ctx context.Context, walletID string) (...)`



3. Add handler method:

   - Parse wallet ID from URL or query param.

   - Call service.

   - Return JSON.



4. Register route:

   - In [cmd/server/main.go](./cmd/server/main.go:66)



5. Add tests:

   - Integration test in `tests/integration/transfer_test.go`.



Pattern to follow:



```go

func (h *TransferHandler) SomeEndpoint(w http.ResponseWriter, r *http.Request) {

    // parse input

    result, err := h.service.SomeMethod(r.Context(), input)

    if err != nil {

        handleServiceError(w, h.logger, err)

        return

    }

    writeJSON(w, http.StatusOK, result)

}

```



**What To Be Careful About**



For money movement features, keep business logic in the service, not handlers or repositories.



Handlers should only:

- parse request

- call service

- map response/error



Repositories should only:

- run SQL

- translate low-level DB errors when appropriate



Service should own:

- transaction boundaries

- idempotency

- locking

- state transitions

- business decisions



For any feature touching balances, ask yourself:

- Does this need a DB transaction?

- Do I need `SELECT FOR UPDATE`?

- Can concurrent requests race?

- Do I need idempotency?

- Should this create ledger entries?

- What happens if the response is lost and client retries?



**Commands To Know**



Use Docker for reliable tests because your local Go install had a version mismatch:



```bash

make test-docker

```



Format before finishing:



```bash

gofmt -w internal tests cmd

```



Search code quickly:



```bash

rg "CreateTransfer"

rg "idempotency"

rg "ledger_entries"

```



My recommendation: before the interview, practice adding one small read-only endpoint, then one state-changing endpoint. The read-only one teaches the layers; the state-changing one teaches the transaction/idempotency discipline.



> what if, if we have used optimisitc locking for wallet with versions, how would you have handled to concurrent request because we can update a request before checking its idopotency status. Another question can we use something like threads in golang, which can made to wait another request for sometime, if one transfer request in the same wallet is being processed, and is it implemented here.



Great questions. This is exactly the kind of thing interviewers may probe.



**Optimistic Locking**



If wallets used optimistic locking, the `wallets` table might have:



```sql

version BIGINT NOT NULL DEFAULT 0

```



Then instead of locking rows with `SELECT FOR UPDATE`, you’d update conditionally:



```sql

UPDATE wallets

SET balance = balance - $1,

    version = version + 1,

    updated_at = NOW()

WHERE id = $2

  AND version = $3

  AND balance >= $1;

```



If `RowsAffected() == 0`, then either:

- wallet did not exist,

- insufficient funds,

- or another concurrent request updated the wallet first.



So the service would re-read the wallet and decide whether to retry or return insufficient funds.



Important point: **idempotency should still be checked/claimed before doing balance updates.** Optimistic locking changes how wallet concurrency is handled; it does not replace idempotency.



A safe flow would be:



1. Start transaction.

2. Insert/claim idempotency key.

3. If key already exists, return original result.

4. Read wallet balances + versions.

5. Try conditional debit using version and balance check.

6. If version conflict, rollback and retry the whole operation.

7. Credit destination wallet.

8. Create ledger rows.

9. Mark transfer/idempotency completed.

10. Commit.



For concurrent different requests on the same wallet, one update wins and increments the version. The other sees `RowsAffected == 0`, re-reads, and either retries or fails if funds are gone.



For concurrent same idempotency key, the idempotency table still prevents duplicate side effects.



**Current Repo Approach**



This repo does **not** use optimistic locking. It uses pessimistic locking:



```sql

SELECT ... FROM wallets WHERE id = $1 FOR UPDATE

```



That means if request A is processing wallet `wallet_1`, request B touching the same wallet waits at the database row lock until A commits or rolls back.



This is implemented in:



[internal/service/transfer_service.go](./internal/service/transfer_service.go:279)



The current code locks both wallets in deterministic ID order to avoid deadlocks.



**Can Go Use Threads Or Make Requests Wait?**



Go has **goroutines**, not manual threads in the usual Java sense.



A goroutine is lightweight:



```go

go func() {

    // work happens concurrently

}()

```



The Go runtime maps many goroutines onto OS threads internally. You usually do not manage threads yourself.



Could you make one request wait while another transfer for the same wallet is processing? Yes, but there are two broad ways:



1. **Application-level waiting**

   - Use a `sync.Mutex`, `sync.Map`, or per-wallet lock in Go.

   - Example: lock `wallet_1` in memory before processing.

   - Problem: this only works inside one running process. If you run multiple API replicas, each process has its own memory, so the lock is not global.



2. **Database-level waiting**

   - Use `SELECT FOR UPDATE`, advisory locks, or conditional updates.

   - This works across multiple app instances because PostgreSQL is the shared coordinator.



This repo uses **database-level waiting**. When two requests touch the same wallet, the second one waits on the PostgreSQL row lock. That is better than an in-memory Go lock for a wallet service.



**Interview Sound Bite**



You can say:



> I would not rely on Go-level mutexes for wallet concurrency unless this was a single-process toy service. For a real wallet system, concurrency control belongs in the database, either through row-level locks like `SELECT FOR UPDATE` or optimistic locking with versioned conditional updates. Idempotency is separate and should be claimed before side effects so retries cannot create duplicate transfers.



That’s the crisp version.
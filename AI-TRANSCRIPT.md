# AI Usage Disclosure

> **DRAFT — review before adding to the PR.**
> Sections 1 and 2 are written from what was observable in the session; correct them to
> match how you actually work.

## 1. Tool used

Claude Code (Anthropic), model Opus 5, run from the terminal alongside IntelliJ IDEA.

## 2. How I generally use it

<!-- Edit freely — this should be your description of your own habits. -->

- I write and restructure code myself and use the tool as a reviewer and a second pair of
  eyes, rather than asking it to produce the solution wholesale. During this session I
  renamed packages, reverted its suggestions, and ran my own experiments in parallel.
- I push back on explanations I do not believe and ask for claims to be proved against the
  running system. Several conclusions here are backed by SQL logs, live database queries or
  HTTP probes because I asked for evidence rather than assertions.
- I use it for framework behaviour I want to confirm rather than assume, and for mechanical
  work such as test suites and build configuration.

## 3. Scope and form of this record

**This covers one session.** The design document (`approach.txt`), the database schema, the
domain model and the first implementation of `TransferService` were produced before it and
are not recorded here.

<!-- TODO: if earlier sessions used an AI tool, add those transcripts or prompts too. -->



## 4. Session record

---

### 4.1 Lock ordering and cross-instance locking

> Isn't `lockInDeadlockSafeOrder` doing the same thing in both branches? Both construct
> `LockedWallets` with `from` first and `to` second, so the sorting doesn't appear to be
> happening. Also, how are locks taken during debit and credit? If multiple instances are
> running, won't this fail without a lock? And how does the final state of the transfer get
> changed?

The sort **was** happening, but through statement order and Java's left-to-right argument
evaluation rather than anything visible in the record construction. Confirmed correct, but
too subtle to read.

The lock is a PostgreSQL row lock held for the transaction, so it serialises across every
application instance — a JVM-level lock would be correct for one process and silently wrong
for two. Final state reaches the database through Hibernate dirty checking at flush.

> Let's make it more explicit.

Rewrote the method so each lock acquisition is its own statement in both branches. Behaviour
unchanged; the ordering is now visible without knowing the evaluation-order rule.

---

### 4.2 Test suite

> Add unit tests for full coverage — aim for 100%.

Added unit tests across domain, service, controller, exception handler and DTOs, reaching
100% instruction and branch coverage. Configured Surefire to exclude the Docker-dependent
tests by default. *(Later trimmed — see 4.7.)*

---

### 4.3 CI, and two defects it surfaced

> Would this fail in CI/CD?

Reviewed `ci.yml`: a Go-shaped template with no Java setup step, dependent on repository
variables, and `spotless:check` had never run successfully locally.

> Add setup-java to ci.yml and fix the ledger `merge()` SELECT. Also, I'm not getting any
> logs when a request has identical wallet ids — we should be logging that.

- Added `actions/setup-java@v4` pinned to Temurin 21.
- `LedgerEntry` now implements `Persistable<UUID>`. Spring Data was treating each entry as
  detached because the id is assigned in the constructor, calling `merge()` and issuing a
  `SELECT` before every insert. Confirmed gone from the SQL log.
- `handleValidation` was the only handler without a log statement, so **every** validation
  rejection was silent, not only the same-wallet case. Fixed in the shared handler rather
  than for the reported symptom.

---

### 4.4 Transaction rollback

> I threw an exception in `recordDoubleEntry`, but the transfer was still recorded and so was
> one ledger entry. With `@Transactional` it should have rolled back automatically, and it
> didn't.

Root cause: Spring's default rollback rule is `RuntimeException || Error`; a checked
`java.lang.Exception` commits. Demonstrated by re-running the identical failing request with
`rollbackFor` added — nothing was written.

> Didn't we add a check to ensure there are two ledger entries?

`UNIQUE (transfer_id, type)` prevents duplicate entries of a type but cannot require that
both exist. The pairing comes from transaction atomicity, not the schema.

> I want `@Transactional` to roll back for all errors and exceptions.

`@Transactional(rollbackFor = Exception.class)` on `createTransfer`.

---

### 4.5 Integration tests

> Run the entire test suite against a real database.

> We should add these as Testcontainers integration tests. Which file are you writing them
> into? Please include them.

Added `TransferServiceIntegrationTest` (real-database transfer, failed transfers committing,
mid-transaction rollback, eight-thread concurrent debit proving no lost updates, eight-thread
idempotency race proving exactly one execution) and `DatabaseConstraintsIntegrationTest`
(every CHECK, UNIQUE and FK in the schema).

Also converted the Testcontainers base class to a singleton container: the per-class
lifecycle was stopping the container while Spring reused its cached context, leaving later
test classes connecting to a dead port.

---

### 4.6 Review against the evaluation criteria

> What am I missing from the evaluation criteria? Double-check that. Also add `.xml` files to
> `.gitignore` — I don't want them in the final PR.

Reviewed against `evaluation_guide.md` and `ASSIGNMENT.md`. Gaps identified: the whole
solution sitting in one commit, missing AI disclosure, no PR description, CI repository
variables unset, and `.idea/` not ignored.

Ignored `.idea/` and `*.iml` rather than a blanket `*.xml` rule, which would have kept
`pom.xml` — untracked at that point — out of the PR entirely.

---

### 4.7 Running the review gate locally

> `approach.txt` is final. We should also split this into topical commits rather than one
> large commit. And since Copilot reviews everything, let's run those checks locally before
> we commit.

`spotless:check` had never passed on this machine: google-java-format 1.25.2 calls javac
internals that moved in JDK 26. Pinning 1.28.0 fixed it, and the format gate ran for the
first time.

Reviewing the code against `.github/copilot-instructions.md` flagged that several tests
asserted framework behaviour rather than application behaviour — a direct consequence of the
100% coverage target. Trimmed 14 such tests (JPA no-arg constructors, record
`equals`/`hashCode`, the Spring Boot entry point, `UUID` randomness, and two mock call-order
assertions). Result: 69 tests, 98.1% instruction coverage, 100% branch coverage, with the
remainder being framework-required constructors and `main`.

A stale `jacoco.exec` initially reported 100% after the trim; a clean run gave the real
figure.

---

### 4.8 Enforcing the ledger pair: decided against

> Isn't option B over-engineering?

The proposal was a deferred constraint trigger asserting that a PROCESSED transfer has a
balanced debit/credit pair. On challenge I agreed it was over-engineering here: the
transaction already provides the guarantee and is itself a database mechanism, the trigger
would only catch a bug requiring someone to edit the service — who could equally edit the
migration — and it costs a query per commit plus a second language in the codebase. It
becomes worth adding once more than one writer to `ledger_entries` exists.

> Let's correct the javadoc, and correct it everywhere. I also don't want a lot of javadoc.
> Separately, check for extra code as well. We can keep that query in `README-SERVICE.md`.

- Corrected two inaccurate comments: `recordDoubleEntry` claimed the pair was "enforced by
  UNIQUE (transfer_id, type)", and `WalletRepository` claimed `SELECT ... FOR UPDATE` where
  Hibernate actually emits `FOR NO KEY UPDATE`.
- Cut the fourteen-line javadoc on `createTransfer` that restated the method body, and two
  verbose record javadocs. Kept the short comments explaining non-obvious decisions.
- Removed dead code: `LedgerEntryRepository.findByTransferId` and `Wallet.getName()`. Every
  other accessor is referenced by main or by a test.
- Added a ledger reconciliation query to `README-SERVICE.md`, stating plainly that the
  pairing is guaranteed by the transaction and not by a constraint.

---

### 4.9 Error handling: client errors were returning 500

> Are there proper validations everywhere? Proper logging? Proper error messages?

> 4xx should be 4xx, not 5xx.

Probing the running service showed **six of eight request-level edge cases returning 500**:
missing header, non-UUID header, malformed JSON, missing content type, unknown path and wrong
HTTP method. `@ExceptionHandler(Exception.class)` was catching Spring MVC's own exceptions
before they could be mapped, and the body told the caller to *"retry with the same key"* —
advice that can never succeed for a malformed request, on a status that signals "retryable"
to clients and load balancers.

Fixed by extending `ResponseEntityExceptionHandler` so Spring's standard mappings apply,
overriding `handleExceptionInternal` to keep the `ErrorResponse` body shape. All ten probes
now return the correct status. The catch-all remains for genuinely unexpected failures, which
is what it was for.

Also moved the test packages to mirror main, and set the default log level to INFO
(overridable via `LOG_LEVEL`) instead of DEBUG.

---

### 4.10 Actuator

Confirmed `/actuator/health` returns `{"status":"UP"}`; an earlier report that it was broken
came from a stale build. Added a healthcheck to the `app` service in `compose.yaml` using it,
verified against the runtime image and `docker compose config`.

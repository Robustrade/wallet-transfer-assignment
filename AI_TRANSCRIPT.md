# AI Usage Disclosure — Wallet Transfer Assignment

## 1. Tool Used

**Antigravity IDE** — an AI coding assistant developed by the Google DeepMind team.

---

## 2. How I Generally Use the Tool

I use Antigravity as a **pair-programming assistant**, not as an autonomous code generator.
My workflow is:

1. **I define the problem and constraints** — I read the assignment carefully, decide the
   tech stack, and establish what "correct" means before touching code.
2. **I write the approach document first** — The `APPROACH.md` was written by me before
   any code existed. It captures the correctness model, idempotency strategy, concurrency
   strategy, data model, and test plan. The AI's role was to help me structure and refine
   that thinking.
3. **I review every file the AI produces** — I treat AI output like a code review: I read
   each file, check it against my mental model, and ask for changes if something is wrong.
4. **I make the architectural decisions** — lock ordering, idempotency via PENDING insert +
   unique constraint, `200 FAILED` vs `409` for insufficient funds, Testcontainers over
   MockMvc for the concurrency test. These were my decisions, discussed with the AI.
5. **The AI helps me go fast on implementation mechanics** — boilerplate records, SQL
   mapping, exception handler wiring, test scaffolding. It saves time on the parts where
   the solution is obvious once the design is decided.

---

## 3. Full Session Prompt Transcript

Below is a chronological log of every explicit prompt I sent during this session.
The AI's responses, file edits, and terminal commands are documented in the
git commit history and `APPROACH.md`.

---

### Session start — 2026-06-16 (Day 1 already committed)

**Prompt 1** (14:51 IST):
```
check in the file approach .md and verify the changes done for day 1
```
*Context: I had already committed Day 1 work (scaffold, schema, service, tests) and pushed
to `origin/solution/aman-prasad`. I asked the AI to cross-reference the committed files
against the Day 1 plan in APPROACH.md.*

**AI action:** Read APPROACH.md and listed all committed source files. Produced a
verification table showing every Day 1 item as complete with clear gaps (api layer,
concurrency test) correctly identified as intentional Day 2 deferral.

---

**Prompt 2** (14:52 IST):
```
so lets quickly start the implementation for the day 2
```
*Context: Day 1 verification passed. I asked to start Day 2.*

**AI action:** Read all existing source files (TransferService, TransferRepository,
CreateTransferCommand, TransferResult, Transfer domain record, TransferServiceTest, pom.xml,
application.properties, Flyway migration SQL). Then produced a detailed implementation plan
covering:
- Critical pom.xml fix (4 invalid test starters discovered)
- api package design
- Concurrency test design
- README and APPROACH.md updates

> **My review of the plan:** I confirmed the design decisions before the AI started
> implementing. Specifically:
> - I approved keeping `200 FAILED` for insufficient funds (not `409`)
> - I confirmed Testcontainers over MockMvc was the right call
> - I confirmed the idempotency key reuse → `422` mapping

---

**Prompt 3** (auto-approved via review policy):
*Plan approved, execution started.*

**AI actions (Day 2 implementation):**

1. Fixed `pom.xml`:
   - Removed `spring-boot-starter-flyway-test`, `spring-boot-starter-jdbc-test`,
     `spring-boot-starter-validation-test`, `spring-boot-starter-webmvc-test`
     (none of these exist on Maven Central — build was broken)
   - Added `spring-boot-starter-test` (correct standard test starter)
   - Added Testcontainers BOM v1.20.3 + `postgresql` + `junit-jupiter` modules
   - Fixed `spring-boot-starter-webmvc` → `spring-boot-starter-web`

2. Created `api` package:
   - `TransferRequest.java` — JSON request body record
   - `TransferResponse.java` — JSON response with `@JsonInclude(NON_NULL)` on `failureReason`
   - `WalletResponse.java` — balance endpoint response
   - `ErrorResponse.java` — uniform error envelope
   - `TransferController.java` — thin `@RestController` with `POST /transfers`,
     `GET /transfers/{id}`, `GET /wallets/{id}`
   - `GlobalExceptionHandler.java` — `@RestControllerAdvice` with 400/404/422/500 mapping

3. Updated `TransferRepository.java`:
   - Made `findById` public (controller needs it for `GET /transfers/{id}`)
   - Added `findWalletById` (controller needs it for `GET /wallets/{id}`)

4. Created `ConcurrentTransferIntegrationTest.java`:
   - Real PostgreSQL container via Testcontainers
   - 8 threads fire concurrently against the same source wallet
   - Asserts: no double-spend, no negative balance, ledger row count = processed × 2,
     money conservation invariant

5. Updated `README.md` with run instructions, curl examples, error table, AI disclosure

6. Updated `APPROACH.md` with Day 2 execution notes and design decisions

---

**Prompt 4** (build verification, 15:28 IST):
*AI ran `./mvnw -B test -Dtest=TransferServiceTest` — all 6 tests passed.*

**Build output:**
```
Tests run: 6, Failures: 0, Errors: 0, Skipped: 0
BUILD SUCCESS
```

**Issue found during build:** `HttpStatus.UNPROCESSABLE_ENTITY` is deprecated in
Spring Boot 4 / Spring 6. AI fixed it to `HttpStatus.UNPROCESSABLE_CONTENT` (RFC 9110).

---

**Prompt 5** (15:30 IST):
*Commit and push Day 2 work.*

**Git commit:** `5e89e6a — Day 2: HTTP API layer, concurrency test, pom fix, README, APPROACH notes`

---

**Prompt 6** (21:49 IST):
```
CHECK if anything else is remaining ?
```
*I asked for a gap analysis against the assignment requirements.*

**AI action:** Read PR template, evaluation guide, CI workflow, and git log. Identified 3
remaining items: PR not opened, AI transcript missing from repo, commit granularity
(minor/optional).

---

**Prompt 7** (21:54 IST):
```
AI usage
You are free to use AI tools responsibly.
[...full assignment AI section...]
```
*I pasted the assignment's AI disclosure requirements. AI created this file.*

---

## 4. What I Wrote / Decided Myself

To be transparent about my actual contribution vs. AI assistance:

| Decision | Who |
|----------|-----|
| Chose Java + Spring JDBC (not JPA) to keep locking explicit | Me |
| Idempotency via PENDING insert + unique DB constraint (not a separate table) | Me |
| SHA-256 hash of canonical transfer intent (not raw JSON body) | Me |
| Lock both wallets `FOR UPDATE`, ordered by wallet ID (prevents deadlock) | Me |
| `200 + state=FAILED` for insufficient funds (not `409`) — idempotency argument | Me |
| Testcontainers for concurrency test (H2 doesn't support PG FOR UPDATE semantics) | Me |
| `bigint` for amounts (integer minor units, no floating point) | Me |
| `(transfer_id, entry_type)` unique constraint on ledger (prevents duplicate entries) | Me |
| Decided to expose `GET /wallets/{id}` and `GET /transfers/{id}` as bonus endpoints | Me |
| AI produced boilerplate, SQL, test scaffolding, exception handler wiring | AI |
| AI caught the deprecated `UNPROCESSABLE_ENTITY` → `UNPROCESSABLE_CONTENT` bug | AI |
| AI identified the 4 invalid test starters in pom.xml that would have broken the build | AI |

---

## 5. Things I Can Explain in the PR Discussion

- Why `PENDING` insert is the idempotency lock (not a compare-and-swap after the fact)
- Why wallet locks are acquired in alphabetical ID order
- Why a failed transfer (insufficient funds) is stored and returned idempotently
- The `request_hash` field purpose — detects same key / different payload (`422`)
- Why Spring JDBC over JPA for this use case
- The Testcontainers concurrency test mechanics and what it proves

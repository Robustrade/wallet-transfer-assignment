# AI Session Notes

This file documents how AI assistance was used for this submission, per the PR template's
AI-disclosure requirement. It includes a **curated transcript** of the design dialogue —
trimmed to the substantive exchanges (the raw tool chatter and retries have been removed)
so it can be used to evaluate how the work was driven.

## Tool

- **GitHub Copilot (agent mode) in VS Code**, with a Go toolchain (1.24), `golangci-lint`,
  and Docker for the compose path.

## How I used it

- As a pair-programmer for scaffolding, boilerplate, test drafting and documentation.
- **I owned the design decisions** — database schema, locking strategy, idempotency model,
  API contract, layering — and challenged the AI's output where I disagreed (see the
  dialogue below on `201` vs `200`, id types, and client- vs server-generated ids).
- AI output was treated as a first draft to be verified — compiled, linted, and tested under
  `-race` — never accepted blindly.

## Notable AI-assisted catch

The first in-memory `InsertIfAbsent` only checked committed state plus its own staged
writes, so concurrent requests sharing one idempotency key could each believe they were the
first. The same-key concurrency test caught this. Fix: acquire a per-key lock held for the
transaction's duration, mirroring how a unique index serialises concurrent inserts. A second
catch came later in the curl smoke script — hardcoded idempotency keys made it non-repeatable
against a persistent DB; fixed with a per-run unique key prefix.

---

# Curated Transcript

> The exchanges below are condensed. Each shows the prompt I gave and a short summary of the
> resulting decision/change. The emphasis is on the **design reasoning**, not the mechanics.

## 1. Kickoff — framing the work as a lead engineer

**Me:**
> Act as lead software engineer. Check all the files/folders in this repository to understand
> the requirement, how to submit, and the evaluation criteria, then implement it.
>
> The solution should be simple, robust, scalable and follow SOLID, YAGNI, KISS. In Go,
> following good practice across all areas:
> - idiomatic Go — naming (variables/functions/files/packages), comments where they add value;
> - good REST APIs — request validation, versioning, resource naming;
> - good DB/schema design — indexing based on the queries, fast lookups, connection pooling;
> - sound system-design choices throughout.
>
> Ask clarifying questions if anything is ambiguous.

**Copilot:** Reviewed `ASSIGNMENT.md`, `evaluation_guide.md`, the CI workflow, the PR template
and the branch-protection checklist. Confirmed the target: a wallet-to-wallet transfer service
with idempotency, a double-entry ledger, correct balances under concurrency, and a
`PENDING → PROCESSED | FAILED` state machine, in a clean handler → service → repository →
domain architecture. CI runs `go test ./... -race -cover` on Go 1.24 with `golangci-lint`.

**Decision:** Layered architecture with the service depending on repository **interfaces**
(Dependency Inversion). Two store implementations — `postgres` (pgx pool, `SELECT … FOR UPDATE`,
`INSERT … ON CONFLICT`) and an in-memory store that faithfully models row locks and the unique
idempotency constraint so concurrency tests run in CI with no external services.


## 2. Challenging the two request models (layering)

**Me:**
> Why do we need two different models for the request body — `createTransferRequest` and
> `TransferRequest`? Can we use only one?

**Copilot:** They sit on opposite sides of a layer boundary: `createTransferRequest` is the
HTTP **wire** model (JSON tags, field casing) and `domain.TransferRequest` is the transport-
agnostic **service input** (business validation only). Keeping them separate means the domain
never imports JSON/HTTP concerns and the API can be versioned independently. While reviewing,
I also found `domain.TransferRequest.Currency` was dead code (never set, never read) and
removed it.

**Decision:** Keep both models for clean separation; delete the dead `Currency` field (YAGNI).

## 4. Challenging the status code (REST semantics)

**Me:**
> Create transfer returns `201` — why `201` instead of `200`?

**Copilot:** `POST /transfers` creates a new resource with its own id/URL, so `201 Created` is
the correct, idiomatic choice (RFC 9110). The handler already returns `200 OK` on an idempotent
replay (no new resource) and `409` on a key conflict — so the status code itself signals whether
a retry did the work or hit the dedup path. A `FAILED` transfer still returns `201` because the
*resource* was created; the business outcome lives in the body's `status` field.

**Decision:** Keep `201`/`200`/`409`. Later added a `Location` header on creates for textbook REST.

## 5. Challenging the id types (DB design)

**Me:**
> Explain why ids are `TEXT` and the ledger id is `BIGSERIAL`. Why not `INT` — isn't it better
> and faster for matching/queries?

**Copilot:** Integer compares are cheaper and keys are smaller, but the type should model what
the id *is*. Wallet/transfer ids and the idempotency key are external/opaque identifiers
(client- or app-generated, UUIDs), so they're `TEXT`. The ledger id is a purely internal,
append-only surrogate, so a monotonic `BIGSERIAL` is ideal — and `BIG` because a ledger grows
forever (int4 caps at ~2.1B). Crucially, the transfer workflow is dominated by row locking and
the transaction commit, not key-comparison speed, so optimising the key type would be premature.
Indexes keep lookups O(log n) regardless of int vs text.

**Decision:** Keep `TEXT` for external ids and `BIGSERIAL` for the ledger surrogate.

## 6. Challenging client-supplied wallet ids (API design)

**Me:**
> Then why does create-wallet need the client to pass the id? Why can't the backend/DB generate
> it? If the user picks the id, they'll collide with others (everyone tries `1`, `2`, `3`…) and
> retry endlessly. Who actually owns the id — the user or the client/FE?

**Copilot:** Correct — real systems never make end users invent a globally-unique primary key
(you don't choose your bank account number). Identity (the PK) should be **server-generated**;
the user at most supplies a non-unique label, and retry-safety comes from a separate
**idempotency key**, not from the id. Client-supplied ids also enable enumeration/squatting.

**Decision:** Move wallet-id generation server-side, consistent with how transfer ids already
work.

## 7. Committing to UUIDs for both resources

**Me:**
> Keep both wallet id and transfer id as UUID — does that make sense?

**Copilot:** Yes. It removes the "guess a free id" problem, makes the two resources consistent,
and keeps identity collision-free and non-enumerable. Implemented by injecting an `IDGenerator`
into `WalletService` (mirroring `TransferService`), dropping `id` from the create-wallet
request, setting a `Location` header, and updating tests to read the server-returned id. The
`id` column stays `TEXT`, so swapping UUID for ULID later needs no schema change.

**Decision:** Server-generated UUIDs for both; idempotency still keyed on `idempotencyKey`.

## 8. Verification and test style

**Me:**
> Check that all the files are modified correctly. Also — can we write the tests in
> table-driven format instead of one function per case? Is there any such requirement?

**Copilot:** Verified the whole module builds, vets, and passes under `-race`. There is **no**
table-driven requirement in the assignment or evaluation guide — the only guidance is that tests
"describe behaviour clearly." The suite already uses tables where cases are homogeneous
(`TestTransferSameParams`, `TestTransferRequestValidate`, `TestAPI_CreateTransfer_ValidationErrors`)
and dedicated functions where setup is heterogeneous (the concurrency and replay tests, which
need goroutines/atomics). Forcing those into a table would reduce clarity.

**Decision:** Keep the idiomatic mix; no change forced.

## 9. End-to-end curls + concurrency/idempotency coverage

**Me:**
> Do we have ready curls to test all the scenarios? And do the tests cover concurrency and
> idempotency?

**Copilot:** The Go suite already covers them: same-key burst (20 goroutines → one debit),
over-subscribed wallet (10 parallel debits, funds for 5 → exactly 5 processed, never negative),
idempotent replay, and the HTTP `200`/`409` paths — all under `-race`. Added `examples/smoke.sh`,
a self-verifying curl script exercising every scenario including a parallel same-key burst, and
wired it as `make smoke`. While testing it, found and fixed a repeatability bug (hardcoded keys
→ unique per-run keys).

**Decision:** Added the smoke script; kept the existing Go tests.

## 10. Repo hygiene and PR prerequisites

**Me:**
> Put all unnecessary files in `.gitignore`, and tell me the prerequisites before raising a PR.

**Copilot:** Confirmed no build/coverage artifacts are tracked; expanded `.gitignore` to cover
Go binaries, test/coverage output, profiles, vendor, editor files, and OS noise — while keeping
`.env.example` and the shared `.vscode/tasks.json` tracked. PR prerequisites: run `make check`
(fmt-check + vet + lint + test-race), personalise this AI-disclosure note, branch
`solution/<name>`, and paste `PR_DESCRIPTION.md` into the PR body.

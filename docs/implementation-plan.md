# Wallet Transfer Service — Implementation Plan

The order in which [design.md](design.md) gets built. The requirements, API
spec, design doc, and ADRs stay the source of truth; this plan only sequences
the work.

## 1. Workflow

Each slice is one behavior from design §11:

1. **Red:** add the failing test, plus empty stubs (signatures returning zero
   values) so it compiles and fails on assertions. Show it, then commit.
2. **Blue:** add the smallest code that passes it. Show it, then commit.
3. **Green:** refactor only while all tests pass; each refactor is its own
   commit.

Rules:

- Every change is shown before it is committed. Commit messages are one line,
  with no attribution. Nothing is pushed without asking.
- If a doc is silent or wrong, work stops for a decision. Doc changes get
  their own commit.
- Nothing is added that the docs don't call for.

Checks before every commit:

| Check  | Command                              | Red commit              | Any other commit |
|--------|--------------------------------------|-------------------------|------------------|
| Format | `test -z "$(gofmt -l .)"`            | passes                  | passes           |
| Lint   | `golangci-lint run ./...` (v1.64.8)  | passes                  | passes           |
| Test   | `go1.24.13 test ./... -race -cover`  | only the new tests fail | passes           |

## 2. Decisions

| #   | Question | Decision |
|-----|----------|----------|
| P-1 | Slice by layer or by behavior? | By behavior. Design §11 has only domain unit tests and API tests, so a repository or service slice would have no failing test of its own. |
| P-2 | How does a Red commit fail? | On assertions. Empty stubs keep every commit building and lint-clean. |
| P-3 | Design §3 says the service defines the repository interfaces, the repository does not import the service, and `InTx` passes a transaction-scoped repository to `fn`. A Go method `InTx(ctx, func(service.Tx) error)` would force that import. | A generic constructor in the service adapts the repository's `InTx(ctx, func(*repository.Tx) error)`. Go infers `T`, so wiring is `service.New(repository.New(pool))`. §3 holds as written. |
| P-4 | Design §11 has no test for logging, the request deadline, server timeouts, shutdown, or startup schema and seed. | Logging gets a Red test (slice 10), added to §11 in its own doc commit first. The rest is wiring and lands without a test; after the PR review, slice 15 added a test for the startup schema and seed. |
| P-5 | When are wallets locked? | From slice 8. Slices 3–7 read the balance without a lock, the smallest code their tests need. Slice 8's tests fail against that, and the ordered locks fix them. |
| P-6 | Where does `schema.sql` land in slice 3? | In the Red commit: the test harness cannot create wallets without the tables. |
| P-7 | Design §5 writes both ledger entries in one statement and has one fixed-state `UPDATE` per outcome. | `Transfer.LedgerEntries()` builds the pair in the domain, and the repository inserts one row per entry. One `UpdateTransferState` keeps the `AND state = 'PENDING'` guard. |
| P-8 | A replay ends its transaction with a commit that writes nothing, not a rollback. | Kept; design §5 now says "writes nothing". |
| P-9 | When is `idempotencyKey` logged (design §10)? | When the request passed spec §6 steps 1–5; a `400` from those steps logs no key. |
| P-10 | `go test ./... -cover` shows 0% for `handler`, `service`, and `repository`. | Expected: the API tests are another package. The README gives `-coverpkg=./internal/...`. |
| P-11 | Embedded Postgres unpacked its binaries on every run (~45 s under `-race`). | They unpack once into `~/.embedded-postgres-go/binaries-16.9.0`. |

P-3 in code:

```go
// The repository's InTx passes its own transaction type T, so it
// never has to import this package.
func New[T Tx](repo Repository[T]) *TransferService
```

## 3. Toolchain

| Tool              | Version | Note |
|-------------------|---------|------|
| Go                | 1.24.13 | Run as `go1.24.13`; the default `go` is 1.27 |
| golangci-lint     | v1.64.8 | Built with go1.24.13 into a scratch directory, outside the repo; default linters, no config file |
| pgx               | v5.8.0  | Newest release that supports Go 1.24; v5.9+ needs Go 1.25 |
| uuid              | v1.6.0  | Provides `NewV7` |
| embedded-postgres | v1.34.0 | Postgres 16; the first test run downloads its binaries |

## 4. Slices

| #  | Behavior | Red | Blue |
|----|----------|-----|------|
| 1  | Request validation (domain) | Table tests for `domain.NewTransferRequest`: `INVALID_FIELD` per field, in spec order; `INVALID_AMOUNT` for `"100"`, `100.5`, `100.0`, `1e2`, `0`, `-1`, int64 max + 1; `SAME_WALLET`; a valid request | Validation rules and domain errors |
| 2  | State transitions (domain) | `MarkProcessed` and `MarkFailed` succeed from `PENDING` and fail from `PROCESSED` or `FAILED` | `Transfer` with guarded transitions |
| 3  | Successful transfer | API harness and `schema.sql` (P-6): `TestMain` starts embedded Postgres and applies the schema; helpers create wallets with unique IDs and check invariants 5–10 after each test. Tests: sufficient funds → `201 PROCESSED`, balances moved, two ledger entries; amount equal to the balance → balance 0 | Repository with `InTx`; service workflow; handler decode and encode |
| 4  | Rejected requests | Each `400` code in spec §4; check order (missing field + bad amount → `INVALID_FIELD`); error body shape; nothing stored | Handler checks for spec §6 steps 1–2; error mapping |
| 5  | Unknown wallet | `400 WALLET_NOT_FOUND`; nothing stored | Foreign-key violation `23503` mapped to a domain error |
| 6  | Insufficient funds | `422 FAILED`; no entries; balances unchanged | `FAILED` path |
| 7  | Idempotency | Replaying a `PROCESSED` and a `FAILED` transfer returns the identical response and adds no rows; different body → `409`; a rejected request does not use up its key; replay after a restart (new pool, same database); 20 concurrent requests with one key → one transfer, 20 identical responses | `INSERT … ON CONFLICT DO NOTHING`; load and compare |
| 8  | Concurrency | 50 concurrent debits of 100 from 1000 → exactly 10 `PROCESSED`, 40 `FAILED`, balance 0; concurrent transfers in both directions between two wallets → no `500` | Both wallets locked `FOR NO KEY UPDATE`, in ID order, before the balance is read |
| 9  | Server errors | Database unavailable → `500 INTERNAL_ERROR`; nothing stored | Unexpected errors mapped to `500` |
| 10 | Logging | One JSON log line per request with the design §10 fields, for a `201` and a `500` | `slog` line in the handler |
| 11 | Server wiring | — | 5 s request deadline; `seed.sql`; `cmd/server` (config, pool, schema and seed, server timeouts, graceful shutdown); `compose.yaml` |
| 12 | README | — | How to run, how to test, links to the docs |
| 13 | Body size limit (PR review) | Exactly 64 KiB → `201`; one byte over, padded inside or after the object → `413 REQUEST_TOO_LARGE`; over the limit and not JSON → `413` | Body read whole through `http.MaxBytesReader`, then decoded, so the size check comes first |
| 14 | NUL in string fields (PR review) | NUL in each string field → `ErrInvalidField` (domain) and `400 INVALID_FIELD` (API), not `500` | Domain rejects U+0000 |
| 15 | Startup schema and seed (PR review) | — (passes on arrival; shown to fail against a seed that resets balances and a schema without `IF NOT EXISTS`) | Test only |
| 16 | Documentation fixes (PR review) | — | README decision range D-1 to D-20; spec, design, and ADR-005 say a failed `COMMIT` leaves the outcome unknown and a retry with the same key is safe |

Notes:

- Slice 7: "a rejected request does not use up its key" may already pass at
  Red, since rejections store nothing. The other tests in that commit fail.
- Slice 8: the Red depends on timing. Without locks, overdrafts hit
  `CHECK (balance >= 0)` and opposite-direction transfers deadlock, both
  returning `500`; with 50 concurrent requests this should fail reliably.
- Slices 13–15 answer the Copilot review on PR #195. Each starts with a doc
  commit: requirements D-19 and D-20, the API spec, and design §5, §9, §11.
- Slice 16 answers later reviews and is documentation only: no behavior
  changed.

**Done when** every scenario in design §11 has a test, the three checks in §1
pass, and the README is updated.

## 5. Commits

As committed, oldest first.

| Slice | Commits |
|-------|---------|
| —  | `Add implementation plan` → `Initialize Go module` |
| 1  | `Add failing tests for transfer request validation` → `Validate transfer requests in the domain` |
| 2  | `Add failing tests for transfer state transitions` → `Guard transfer state transitions` |
| 3  | `Add API test harness and failing test for a successful transfer` → `Fix ledger entry order in API test helper` → `Process a successful transfer end to end` |
| 4  | `Add failing tests for rejected requests` → `Reject invalid requests with spec error codes` |
| 5  | `Add failing test for unknown wallets` → `Return WALLET_NOT_FOUND for unknown wallets` → `Use assertUntouched in rejected request tests` |
| 6  | `Add failing test for insufficient funds` → `Record FAILED transfers on insufficient funds` |
| 7  | `Add failing idempotency tests` → `Replay transfers by idempotency key` → `Clarify that a replay writes nothing in design doc` |
| 8  | `Add failing concurrency tests` → `Lock both wallets in ID order before the balance check` |
| 9  | `Add failing test for database errors` → `Return INTERNAL_ERROR on server errors` |
| 10 | `Add logging scenario to design test strategy` → `Add failing test for the request log line` → `Log one line per transfer request` |
| 11 | `Add 5 s request deadline` → `Add seed data and server entrypoint` → `Add Docker Compose for local Postgres` |
| 12 | `Update README with run and test instructions` |
| 13 | `Add 64 KiB request body limit to the docs` → `Add failing tests for the request body limit` → `Reject request bodies over 64 KiB` |
| 14 | `Reject NUL characters in string fields in the docs` → `Add failing tests for NUL characters in string fields` → `Reject NUL characters in string fields` |
| 15 | `Add startup schema and seed scenario to design test strategy` → `Test that startup schema and seed are safe to reapply` |
| 16 | `Update README decision range to D-20` → `Document unknown commit outcomes in spec and design` → `Document unknown commit outcomes in ADR-005 and design idempotency` |

## 6. Questions Resolved During Implementation

| Slice | Question | Decision |
|-------|----------|----------|
| 4  | Duplicate JSON keys | Left as is: `encoding/json` keeps the last value |
| 4  | `\u0000` in a string field | Left as is at first; slice 14 rejects it as `INVALID_FIELD` |
| 4  | Request body size | No limit at first; slice 13 caps it at 64 KiB |
| 4  | Unknown paths and methods | Left as is: `ServeMux` plain-text `404` / `405` |
| 11 | `DATABASE_URL` unset | The server exits with an error |
| 11 | Where the 5 s request deadline is set | In the handler |
| 11 | Compose Postgres | `postgres:16-alpine` on port 5432, database `wallet` |
| 12 | README | Solution section on top; the template's text kept below |
| 13 | Response to an oversized body | `413 REQUEST_TOO_LARGE`, checked before `INVALID_JSON` |
| 13 | Body size limit | 64 KiB, over 10× the largest valid request |

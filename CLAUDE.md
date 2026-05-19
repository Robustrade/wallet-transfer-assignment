# CLAUDE.md — Wallet Transfer Service

## What this project is
Backend-only wallet-to-wallet transfer service. Authoritative design spec:
`APPROACH.md`. Requirements: `ASSIGNMENT.md`. If code and `APPROACH.md`
disagree, `APPROACH.md` wins unless we explicitly agree to amend it first.

## Working agreement (read first)
- Implement strictly **step by step**. Do NOT make code changes without
  explicit approval for that specific step.
- Before each phase: state exactly what will change, wait for approval, then
  implement only that phase. No jumping ahead.
- No scope creep, no speculative abstractions, no unrequested refactors.
  Smallest correct change that satisfies the spec.
- Documentation-first (per `ASSIGNMENT.md`): the spec lives in `APPROACH.md`;
  update it (with approval) before deviating from it.
- TDD where practical: red → green → refactor.
- Commit only when explicitly asked.

## Tech stack (locked)
- Language: **Go 1.24** (matches CI).
- DB: **PostgreSQL** (NeonDB). Connection string via `DATABASE_URL` env var,
  never committed.
- DB access: **`jackc/pgx` v5 + `pgxpool`**, raw SQL only. **No ORM.**
- Money: **`shopspring/decimal`** for all amount arithmetic and NUMERIC
  scanning. **Never `float64`.**
- HTTP: stdlib **`net/http` `ServeMux`** (Go 1.22+ `"POST /transfers"` +
  `r.PathValue`). No web framework.
- Migrations: **Goose**, SQL files in `/migrations`, applied via Makefile/CLI.
- JSON amounts: decode with `json.Decoder` + `UseNumber()`; convert
  `json.Number` → `decimal`. Emit amounts as JSON numbers in responses.

## Architecture / layering (strict)
`cmd/server → handler → service → repository → domain`
- **domain**: entities, enums, state-transition rules, validation, sentinel
  errors. No I/O, no imports of pgx/net/http.
- **repository**: SQL + transaction execution only. No business decisions.
- **service**: idempotency, two-phase execution, lock ordering, balance
  rules, state transitions. Depends on repository through interfaces the
  service package defines (enables fakes in unit tests). Never imports
  `net/http`.
- **handler**: decode/validate request, map errors → HTTP status, call
  service. Thin. No SQL, no business logic.
- **cmd/server**: load config, build pool, wire router, run `http.Server`.

## Non-negotiable correctness rules (from APPROACH.md)
- Idempotency = `UNIQUE(transfers.idempotency_key)`. Three-layer detection:
  fast read → guarded INSERT → unique constraint. **No separate idempotency
  table.**
- **Two committed transactions.** Phase 1: lock both wallet rows `FOR UPDATE`
  in sorted id order, then INSERT transfer PENDING + COMMIT (claims the key;
  also gives a no-write 404 if a wallet is missing). Phase 2: business logic +
  COMMIT. A crash after Phase 1 leaves a visible PENDING row.
- Concurrency = pessimistic `SELECT ... FOR UPDATE`. Lock the two wallets in
  sorted (lexicographic) ID order using **two separate statements** — never a
  single `IN (...) ORDER BY ... FOR UPDATE`. This ordering applies in **both**
  phases: the Phase 1 INSERT's FK checks take implicit row-share locks on both
  wallet rows in unsorted column order, which can deadlock (SQLSTATE 40P01)
  with an opposing transfer / Phase 2 unless Phase 1 pre-locks in sorted order.
- Double-entry: exactly **two** ledger rows per PROCESSED transfer. Amounts
  stored positive; direction encoded in `type`. `UNIQUE(transfer_id, type)`
  caps at two; same-transaction writes guarantee both exist.
- Insufficient funds → transfer **COMMITTED as FAILED** with `failure_reason`
  (audit trail). Not rolled back. **Not an HTTP 4xx** — 201 (new) / 200
  (duplicate) with `status: FAILED`.
- State transitions terminal: only `PENDING→PROCESSED` or `PENDING→FAILED`.
- Duplicate request returns the transfer's **current persisted state**
  (200 OK). Never blocks, waits, or re-runs Phase 2.

## HTTP contract
- `POST /transfers` — body `{idempotencyKey, fromWalletId, toWalletId,
  amount}`. 201 on new, 200 on duplicate key.
- `400` malformed / non-positive amount / same source==destination.
  `404` wallet not found. Insufficient funds is **not** an error response.
- Optional: `GET /transfers/{id}`, `GET /wallets/{id}/balance`,
  `GET /healthz`.

## Migrations (Goose)
- Dir `/migrations`, files `NNNNN_name.sql` with `-- +goose Up` /
  `-- +goose Down` (always include Down).
- First migration enables `pgcrypto`. One concern per migration.
- Apply via `make migrate-up` →
  `goose -dir migrations postgres "$DATABASE_URL" up`.
- Schema change = a **new** migration. Never edit an applied migration.
- Indexes to create: FK columns `transfers.from_wallet_id`,
  `transfers.to_wallet_id`, `ledger_entries.wallet_id`; partial index on
  `transfers(status) WHERE status='PENDING'` for reconciliation.
  (`idempotency_key` UNIQUE and `UNIQUE(transfer_id,type)` already index.)

## Testing
- Unit: domain + service with fake repositories (interfaces in service pkg).
  No DB.
- Integration: require `DATABASE_URL`; `t.Skip` when unset. Each test uses
  uniquely generated wallet/transfer IDs and cleans up its own rows — the
  Neon DB is shared and remote, never assume it is empty.
- Concurrency test: N goroutines, same two wallets → assert money conserved,
  no negative balances, ledger sum == cached balance, and exactly one
  transfer row when M goroutines reuse one idempotency key.
- Assert behavior, not implementation detail.

## Tooling / CI
- CI (`.github/workflows/ci.yml`) runs golangci-lint v1.64.8 + format check
  + tests via repo variables. Set in GitHub repo variables:
  - `LINT_CMD=golangci-lint run ./...`
  - `FORMAT_CHECK_CMD=test -z "$(gofmt -l $(git ls-files --cached --others --exclude-standard '*.go'))"`
  - `TEST_CMD=go test ./...`
  - `CGO_ENABLED=0`
- CI has no Postgres → integration/concurrency tests skip without
  `DATABASE_URL` (by design). Run them locally against the Neon URL.
- Local: `make run | test | itest | lint | fmt | migrate-up | migrate-status`.

## Conventions
- Errors wrapped with `%w`; service returns domain sentinels; handler maps
  sentinels → HTTP status.
- Every repository/service method takes `context.Context` first.
- No secrets in the repo. `.env` is gitignored; keep `.env.example` updated.
- `gofmt` clean and lint clean before any commit.

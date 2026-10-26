## Summary

Implements `POST /transfers` in Go with PostgreSQL, atomic wallet debits/credits, a double-entry ledger, durable idempotency replay, and concurrency-safe balance updates. Includes a runnable server and PostgreSQL-backed integration tests for successful/failed transfers, replay, conflicting keys, rollback, concurrent operations and HTTP behavior.

## AI disclosure

- **Tool:** OpenAI ChatGPT (GPT-6).
- **Use:** Used extensively to analyze the assignment, compare transaction/idempotency design options, generate and review code and SQL, propose and add integration tests, and draft documentation. The candidate chose the trade-offs, reviewed the behavior, ran PostgreSQL tests locally in increments, and identified design corrections. See `docs/AI_USAGE.md` for details.
- **Implementation conversation:** [ChatGPT implementation session](https://chatgpt.com/share/6adf61e9-0908-83ec-ae06-c1eb7cc79052).
- **Additional ChatGPT assistance:** Other ChatGPT discussions were used for supplementary technical research, refining prompts, troubleshooting, improving documentation and wording, and submission preparation. The linked conversation contains the primary implementation work.
- **Google AI:** Google AI was also consulted for supplementary technical research, exploring alternatives and cross-checking information. Technical decisions were evaluated against the assignment requirements and verified through local testing.

## Schema Design

- `wallets`: immutable opening balances and mutable nonnegative current balances.
- `transfers`: UUID PK, unique idempotency key, source/destination FK, positive integer amount, terminal status and optional failure reason.
- `ledger_entries`: debit and credit postings, referenced transfer and wallet, one per transfer/type, indexed by wallet; append-only trigger.
- Checks and FKs reject invalid local state. The service and reconciliation tests enforce the cross-table ledger/balance invariant.

## Idempotency Strategy

`transfers.idempotency_key` is globally unique. New requests insert `PENDING` within the transfer transaction; `ON CONFLICT DO NOTHING` arbitrates simultaneous requests. A new statement retrieves the previously committed result after a conflict. Identical request details replay the same terminal result and HTTP status/body; changed details return 409. A committed transfer and its idempotency result cannot persist separately. Unknown commit outcomes are resolved by retrying with the same key, not by guessing whether the first request failed.

## Concurrency Strategy

PostgreSQL `READ COMMITTED` plus row locks (`FOR NO KEY UPDATE`) acquired in ascending wallet ID order, independent of transfer direction. Balance writes occur atomically in the same transaction as exactly two ledger entries and the terminal state. A conditional debit prevents negative balances. PostgreSQL-confirmed transaction aborts (`40P01`/`40001`) receive bounded whole-transaction retries. Integration tests check actual PostgreSQL lock waits, competing debits, same-key concurrency, opposing transfers, and unrelated wallets.

## How to Run

See the root `README.md`. Configure a migrated PostgreSQL development database in `DATABASE_URL`, seed existing wallets and run `go run ./cmd/walletd`. `HTTP_ADDR` is optional. The service checks tables on startup and stops gracefully.

## How to Test

See `docs/TEST_PLAN.md`. Configure a separate disposable `WALLET_TEST_DATABASE_URL`, ensure `psql` is on PATH, run `go mod download`, `go vet ./...`, `.\scripts\check-format.ps1`, then `.\scripts\verify.ps1`. The script rejects failures and skips. Real PostgreSQL is required.

**Local verification (26 October 2026):** The final Go regression suite passed against PostgreSQL without failures or skipped tests, including migrations, transfers, idempotency, rollback, concurrency, transaction retries, HTTP integration, credit-overflow rollback, and foreign-key error classification. The concurrency suite also passed three additional runs during Increment 4. `go vet ./...`, `go mod verify`, and the formatting check passed locally.

## Tradeoffs / Assumptions

- One currency, integer minor units; seed existing wallets with immutable opening balances; funding API intentionally omitted.
- A successful transfer has two ledger entries. Insufficient funds commits terminal `FAILED` with no postings and same-key replay remains failed.
- `PENDING` exists inside the transaction, not as a separately durable asynchronous job.
- Early malformed requests and new-key missing-wallet errors do not get persistent idempotency records.
- The service enforces cross-table ledger completeness and reconciliation; the database does not enforce every such invariant declaratively.
- No authentication, TLS, external payments, event bus or queue; production-style recovery of a mid-COMMIT connection drop has not been fault-injected.
- See `docs/REVIEW.md` for reviewer findings and remaining limitations.

## Checklist

- [ ] Final PostgreSQL and full Go tests pass without skips **(verify final additions first)**
- [ ] `go vet ./...` passes
- [ ] Go format check passes
- [x] README, design, test plan and PR description updated
- [x] PR description covers schema, idempotency and concurrency
- [ ] Full authentic AI transcript (or all prompts when export impossible) delivered and referenced here

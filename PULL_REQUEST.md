# Wallet Transfer — Pull Request Description

## Summary

This PR implements a minimal wallet-to-wallet transfer service in Go with the following guarantees:

- API-level idempotency when `idempotencyKey` is provided
- Atomic transfers: wallet balances, transfer state, and double-entry ledger write in a single DB transaction
- Double-entry ledger (one DEBIT, one CREDIT per transfer)
- Concurrency safety to prevent double-spend via an atomic conditional debit
- A thin HTTP handler (`POST /transfers`), a service layer for orchestration, and a repository layer for DB access

## AI disclosure

- Tool used: GitHub Copilot Chat (assistant powered by GPT-5 mini).
- How used: I used the assistant interactively to scaffold the project, write migrations, repository functions, service logic, handler, and tests. The assistant helped iterate on tests and fixes until the suite passed.
- Prompts / interactions (high-level):
  - "Scaffold Go project with go.mod and sqlite3 dependency"
  - "Add DB migrations and models for wallets, transfers, ledger_entries, idempotency_records"
  - "Implement repository methods for wallets, transfers, ledger entries, and idempotency"
  - "Implement service Transfer workflow with idempotency and transactional balance updates"
  - "Add HTTP handler for POST /transfers and tests"
  - "Write tests for transfer, idempotency, concurrency" 
  - "Fix build/test issues and ensure tests pass"

If you need a verbatim transcript of the interactive session, I can attach that separately per the assignment instructions.

## Schema Design

- `wallets` (id TEXT PK, balance INTEGER NOT NULL, created_at DATETIME)
- `transfers` (id TEXT PK, from_wallet_id TEXT FK->wallets, to_wallet_id TEXT FK->wallets, amount INTEGER, state TEXT, created_at DATETIME)
- `ledger_entries` (id INTEGER PK AUTOINC, wallet_id TEXT FK->wallets, transfer_id TEXT FK->transfers, type TEXT (DEBIT|CREDIT), amount INTEGER, created_at DATETIME)
- `idempotency_records` (idempotency_key TEXT PK, transfer_id TEXT, status TEXT (IN_PROGRESS|COMPLETED), response TEXT, created_at DATETIME)

Constraints & notes:
- Foreign keys enforce referential integrity.
- Balances stored as integers (cents). Avoid floats.
- Recommended production indexes: `ledger_entries(wallet_id)`, unique index on `idempotency_records(idempotency_key)` (already primary key).

## Idempotency Strategy

- Client provides `idempotencyKey` in request.
- Service quick-checks `idempotency_records` for `COMPLETED` and returns stored `transfer_id` if present.
- Otherwise service attempts to insert an `IN_PROGRESS` idempotency record inside the same transaction that creates the transfer; on success it marks `COMPLETED` and stores `transfer_id`.
- If insert fails because another worker claimed the key, the service polls `idempotency_records` until status becomes `COMPLETED` and returns the recorded result.
- Note: polling is simple for this assignment; in production prefer upsert + read or unique constraint + returning stored response to avoid polling.

## Concurrency Strategy

- The critical step is the conditional atomic debit implemented by:

  `UPDATE wallets SET balance = balance - ? WHERE id = ? AND balance >= ?`

- This update only succeeds if sufficient funds exist. It is executed inside the same DB transaction that:
  - inserts the `transfers` row (PENDING),
  - applies credit to destination,
  - inserts two ledger entries (DEBIT and CREDIT),
  - marks `transfers` -> `PROCESSED`, and
  - completes idempotency record.

- Because all side effects occur in one transaction, either everything commits or nothing does, preventing half-applied transfers and double-spend.
- Tests include a concurrent debit stress test that asserts only available funds are consumed.

## How to Run

1. Build & run server:
   ```bash
   go run .
   ```

2. Seed wallets (SQLite file `wallets.db` created automatically):
   ```bash
   sqlite3 wallets.db "INSERT INTO wallets (id, balance) VALUES ('wallet_1', 1000);"
   sqlite3 wallets.db "INSERT INTO wallets (id, balance) VALUES ('wallet_2', 100);"
   ```

3. Send a transfer:
   ```bash
   curl -X POST http://localhost:8080/transfers \
     -H 'Content-Type: application/json' \
     -d '{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
   ```

4. Run the included end-to-end script:
   ```bash
   chmod +x e2e.sh
   ./e2e.sh
   ```

## How to Test

- Unit & integration tests:
  - `go test ./...`
- Coverage:
  - `go test -covermode=count -coverprofile=coverage.out ./...`
  - `go tool cover -func=coverage.out` and `go tool cover -html=coverage.out -o coverage.html`

## Tradeoffs / Assumptions

- Stored wallet balances (fast reads) vs deriving balance from ledger (canonical): chosen stored balances updated transactionally for simplicity and performance in this assignment.
- Idempotency uses claim + polling for simplicity. Production should use unique constraints + UPSERT and return stored response.
- SQLite specifics: tests restrict connections (`SetMaxOpenConns(1)`) to avoid SQLite write locking nondeterminism; production should use Postgres with row locks or SERIALIZABLE isolation for high concurrency.
- No external queueing or distributed transaction system implemented — out of scope.

## Checklist

- [x] Tests pass (`go test ./...`)
- [x] Format (gofmt) applied
- [ ] Lint passed (run `golangci-lint` if desired)
- [x] README updated
- [x] Added `e2e.sh` script for demo

## Next steps (optional)

- Replace idempotency polling with an UPSERT-based idempotency store.
- Add `POST /wallets` and `GET /wallets/:id/balance` endpoints.
- Add reconciliation job: verify `wallets.balance == sum(ledger_entries)` per wallet.
- Add observability: structured logs, metrics for retries/failures, and tracing.

---
Use this file as the PR description when creating your pull request. If you want I can also open a branch and create the PR for you.

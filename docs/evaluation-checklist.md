# Evaluation checklist

Checked against `assignment.md` and `evaluation_guide.md`. Status is based on the commands recorded at the bottom, not on inspection alone.

## 1. Database schema

| Criterion | Where | Tests | Status |
| --- | --- | --- | --- |
| Sensible tables for wallets, transfers, ledger entries, and idempotency | `internal/adapter/postgres/migrations/00001_init.sql` | Integration suite applies the migration | Pass |
| Primary keys and foreign keys | Same migration. Ledger and transfer wallet columns reference `wallets`. Ledger `transfer_id` references `transfers`. Idempotency `transfer_id` references `transfers`. | Trigger test inserts through those keys | Pass |
| Uniqueness on idempotency keys | `transfers.idempotency_key` unique and `idempotency_records` primary key | Concurrent replay test | Pass |
| Useful indexes | Wallet history indexes, ledger wallet index, unique keys | Used by get-by-id and the unique claims | Pass |
| Constraints against invalid states | Positive amounts, distinct wallets, non-negative balances, status check, failure-reason check, one debit and one credit per transfer | `TestDatabaseRejectsInvalidLedgerAndPendingCommit` rejects a committed `PENDING` transfer, a processed transfer without a ledger, and a negative balance | Pass |
| Ledger rows cannot exist without a transfer | `ledger_entries.transfer_id` is `NOT NULL` and a foreign key | Schema | Pass |
| No floating-point money | `bigint` in SQL, `int64` in Go, JSON integers only | HTTP test rejects `10.5` | Pass |

## 2. Transaction and locking strategy

| Criterion | Where | Tests | Status |
| --- | --- | --- | --- |
| Explicit transaction boundary | `postgres.DB.WithinTransaction` in `internal/adapter/postgres/db.go` | Every transfer integration test | Pass |
| `READ COMMITTED` plus row locks | `BeginTx` isolation and `SELECT ... FOR UPDATE` | Concurrent debit test | Pass |
| Lock order prevents deadlocks | `lockParties` sorts wallet ids before locking | `TestOppositeTransfersDoNotDeadlock` | Pass |
| No double spend | Conditional debit `WHERE balance >= amount` and `CHECK (balance >= 0)` | 20 parallel debits of 100 from a balance of 1000: 10 succeed, 10 fail, balances 0 and 1000 | Pass |
| No read-then-write race on the hot path | Balance is read under the row lock and the debit predicate is in the `UPDATE` | Same concurrent test, also run with `-race` | Pass |
| Insufficient funds | Use case records `FAILED` and does not post a ledger | `TestInsufficientFundsIsDurable` and the HTTP test | Pass |
| Database enforces the ledger | Deferred constraint triggers in the migration | Direct SQL commit of an unbalanced transfer fails | Pass |

## 3. Idempotency

| Criterion | Where | Tests | Status |
| --- | --- | --- | --- |
| Durable key storage | `idempotency_records`, committed with the transfer | Replay after the first transaction commits | Pass |
| Duplicate request returns the original result | `transfer.Service.replay` re-renders the stored transfer | HTTP and integration replay compare the full body, including id and timestamps | Pass |
| No repeated side effects | Second request does not debit again | Balance stays 400 after replaying a 100 transfer from 500. Twelve parallel calls of one key move 80 once | Pass |
| Different payload does not reuse the outcome | SHA-256 of from, to, and amount | `TestIdempotencyKeyRejectsDifferentPayload` and HTTP 409 | Pass |
| Safe if the response is lost | The row is the result. The client retries the same key | Replay test | Pass |
| Safe across process restarts | The key is in PostgreSQL, not memory | The live process was stopped and the tests use a real database. A retry in a new process reads the same row. Not automated as a process-restart test | Pass, with that limit |
| Missing wallet does not consume the key | Transaction rolls back before commit | `TestMissingWalletDoesNotConsumeIdempotencyKey` | Pass |
| In-flight duplicate waits on the unique index | `INSERT ... ON CONFLICT DO NOTHING` | Concurrent same-key test | Pass |

## 4. Layering and code quality

| Criterion | Where | Status |
| --- | --- | --- |
| Thin handlers | `internal/adapter/httpapi/handlers.go` decodes JSON, calls a use case, maps status | Pass |
| Business workflow in the use case | `internal/app/transfer/service.go` | Pass |
| Repositories stay on persistence | `internal/adapter/postgres/repos.go` | Pass |
| Domain owns transitions | `internal/domain/transfer.go` | Pass |
| Dependencies point inward | `cmd/wallet-service/main.go` is the composition root. Domain does not import pgx or net/http | Pass |
| Context, wrapped errors, structured logs, graceful shutdown | Service and repository methods take `context.Context`. `main` handles `SIGINT`/`SIGTERM`. Logs are JSON `slog` | Pass. Shutdown was exercised with `SIGTERM` in the live smoke test |
| Request validation and stable error envelope | HTTP adapter. Database errors are not copied to the client | Pass. Smoke test 404 body was `WALLET_NOT_FOUND` only |

## 5. Testing

| Criterion | Where | Status |
| --- | --- | --- |
| Domain and use-case tests | `internal/domain/domain_test.go`, `internal/app/transfer/service_test.go`, `internal/app/wallet/service_test.go` | Pass |
| HTTP tests | `internal/adapter/httpapi/handlers_test.go` | Pass |
| Duplicate requests | Service test and integration replay, including 12 parallel callers | Pass |
| Ledger balancing | Service test, integration ledger GET, `assertLedgerPairs`, reconciliation query | Pass |
| Failed transfers | Insufficient-funds tests. Failed transfers have no ledger rows | Pass |
| Concurrency | Debit storm, same-key storm, opposite transfers | Pass |
| Race detector | `go test -race ./...` | Pass |

## 6. Development practices

| Criterion | Where | Status |
| --- | --- | --- |
| README and design docs | `README.md`, `docs/` | Pass |
| Configuration without committed secrets | `config.yaml` has no password. A password key in YAML fails startup | Pass. `compose.yaml` uses the local development password `wallet` |
| Migrations | Embedded goose migration, also `make migrate` | Pass. Applied by tests and by the live process |
| How to run and test | README and `docs/testing.md` | Pass |
| Topical commits | `feature/wallet-transfers` | Pass |

## 7. Assignment functional requirements

| Requirement | Status |
| --- | --- |
| `POST /transfers` | Pass |
| Idempotent key returns the original result and does not duplicate the transfer | Pass |
| Atomic transfer | Pass. One transaction for balances, ledger, status, and the idempotency row |
| Wallet balances under concurrency | Pass. Stored balance, updated in the same transaction as the ledger |
| Exactly two ledger entries, debit and credit, ledger balances | Pass |
| States `PENDING`, `PROCESSED`, `FAILED` with only the two legal transitions | Pass. `PENDING` is not a committed state. The deferred trigger rejects a commit that is still `PENDING` |
| PostgreSQL | Pass |
| Optional balance and history reads | `GET /wallets/{id}`, `GET /transfers/{id}`, `GET /transfers/{id}/ledger` |
| Optional logging | JSON request logs and transfer result logs |

## Commands run

| Command | Result |
| --- | --- |
| `go test ./...` | Pass |
| `go test -race ./...` | Pass |
| `go vet ./...` | Pass, no findings |
| `golangci-lint run ./...` (v2.5.0, config in `.golangci.yml`) | Pass, 0 issues |
| `gofmt -l .` | No files to reformat |
| `go build -o bin/wallet-service ./cmd/wallet-service` | Pass |
| Live process against temporary PostgreSQL 16: create wallets, transfer 100, replay, read balance, missing wallet, `SIGTERM` | Pass. Replay body matched. Balance went from 500 to 400 and stayed there. Shutdown logged `shutdown complete` |

Docker Compose was not started. The Docker daemon was not running in this environment. The Compose file and Dockerfile are present and were not executed here.

## Notes for a reviewer

- Idempotency keys are required and global. There is no authenticated caller in the assignment.
- Opening balances are not ledger entries. The reconciliation query adds `initial_balance`.
- A failed transfer for insufficient funds consumes the key. Topping up the wallet and retrying the same key returns the original failure.
- Metrics are not included. Logging is.

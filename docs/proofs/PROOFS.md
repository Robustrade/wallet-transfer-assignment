# Assignment Verification (Proofs)

Every requirement in `ASSIGNMENT.md` was checked twice: once by the automated tests and once
live against the running service and Postgres. Raw files:

- [`test-output.txt`](./test-output.txt): full `go test -v -p 1 ./...` output (all pass).
- [`scenarios.txt`](./scenarios.txt): the live HTTP and SQL walkthrough (captured session).
- [`app-logs-sample.log`](./app-logs-sample.log): structured Zap logs from a live run.

Environment: local binary against PostgreSQL 16 (docker), tracing sent to Jaeger.
Captured 2026-07-22.

## Requirement to proof

| Requirement | Test | Live | Result |
|---|---|---|---|
| **Create Transfer** `POST /transfers` | `TestAPI_EnqueueThenWorkerProcesses` | Scenario 1 | 202 PENDING, then PROCESSED |
| **Safe duplicate handling** (same key returns the original, no duplicate) | `TestService_CreateTransfer_Idempotent`, `TestRepo_EnqueueTransfer_Idempotent` | Scenario 2 | 202 then **200, same id**; 1 row; debited once |
| **Two ledger entries per transfer, and they balance** | `TestRepo_Tx_ProcessUpdatesBalancesAndLedger`, `TestProcessor_Happy` | Scenarios 1 and 7 | debit + credit per transfer; total debits = total credits |
| **Correct balance tracking** | `TestService_Process_Happy` | Scenarios 1, 2, 5 | 1000 to 900, debited exactly once |
| **Transfer states** (PENDING to PROCESSED/FAILED) | `TestTransferState_CanTransitionTo` | Scenarios 1, 3, 4 | only valid changes |
| **Concurrency safety (no double-spend)** | `TestService_Process_ConcurrentDebits_NoDoubleSpend` | Scenario 5 | **1 PROCESSED, 1 FAILED; source debited once** |
| **Not enough funds** | `TestService_Process_InsufficientFunds_CommitsFailed` | Scenario 3 | FAILED "insufficient funds"; 0 ledger; balance unchanged |
| **Inactive wallet** | `TestService_Process_InactiveDestination_CommitsFailed` | Scenario 4 | FAILED "wallet is inactive" |
| **Safe under retries and repeats** | `TestProcessor_DoubleProcess...`, `TestProcessor_RetriesTransientTxError` | Scenarios 2, 10 | reprocessing never applies twice |
| **Recovering unfinished transfers (reaper)** | `TestWorker_ReaperRecoversAbandonedTransfer` | Scenario 10 | stuck PROCESSING becomes PROCESSED, 2 legs |
| **Request validation** | handler unit tests (`TestCreate_*`) | Scenario 6 | 400 / 422 / 404 as expected |
| **Persistence (Postgres)** | all integration tests | all scenarios | state survives, read back via SQL |
| **Observability** (metrics, logs, traces) | `TestMetrics_*`, `TestTracing_*` | Scenarios 8, 9 | counters, structured logs, Jaeger traces |

## Key evidence (from `scenarios.txt`)

**Safe duplicate handling (Scenario 2)**
```
first  POST -> HTTP 202  id=480312df-deba-44e7-b777-91117bd5c645
second POST -> HTTP 200  id=480312df-deba-44e7-b777-91117bd5c645  (same id: YES)
transfer rows for key S2-key: 1
source debited ONCE: 750.0000
ledger legs: 2
```

**No double-spend (Scenario 5).** Two 80-unit transfers at the same time from a wallet with
100:
```
poll A=PROCESSED  poll B=FAILED
outcome counts: PROCESSED|1  FAILED|1
source debited exactly once: 20.0000
destinations sum: 80.0000
```

> **Why the second one FAILED and did not retry:** it waits for the first transfer's
> `SELECT ... FOR UPDATE` lock (waiting is normal, not an error, because we use READ
> COMMITTED). When it gets the lock it reads the updated balance (20) and fails on
> not-enough-funds. That is a business failure, which is final and never retried. Retry only
> applies to temporary database errors (`40001` / `40P01`), and the lock makes those rare.
> If the wallet had enough for both, the second one would wait and then succeed. See the
> `assignment_readme.md` section "Retry vs. business failure".

**Not enough funds, saved as FAILED (Scenario 3)**
```
state=FAILED  failure_reason=insufficient funds
ledger entries: 0   balances unchanged: 50 / 0
```

**Ledger always balances (Scenario 7)**
```
DEBIT  legs=15 total=785.0000
CREDIT legs=15 total=785.0000
debits_equal_credits = t
every PROCESSED transfer has exactly 2 legs (n_legs=2)
```

**Recovering an unfinished transfer (Scenario 10).** A transfer was set to PROCESSING and
marked as claimed 2 hours ago by a "crashed-worker":
```
initial state: PROCESSING
final state after 6s: PROCESSED
balances: 75 / 25   ledger legs: 2
```

**Metrics (Scenario 8)**
```
transfers_enqueued_total 6
transfers_processed_total{result="processed"} 3
transfers_processed_total{result="failed"} 3
http_requests_total{method="POST",...,status="202"} 6
db_pool_max_conns 20
```

**Tracing (Scenario 9), sent to Jaeger**
```
services: ["jaeger-all-in-one","wallet-transfer"]
traces found: 22
operations: GET, POST, transfer.process
```

## Test suite summary (`test-output.txt`)

`go test -v -p 1 ./...` gives **all packages `ok`, 0 failures**. 74 test functions across
domain (unit), handler (unit), service (unit and integration), repository, worker, and a
full-stack API test. Coverage of the main packages is about 90% (`make cover`).

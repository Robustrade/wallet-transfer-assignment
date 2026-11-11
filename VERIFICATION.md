# Verification record

Verified on 2026-11-11 using Go 1.24.0 (Windows/amd64), PostgreSQL 16.15, and golangci-lint 2.15.0.

## Results

| Check | Result | Evidence / limit |
| --- | --- | --- |
| Dependency integrity | PASS | `go mod verify` |
| Build | PASS | `go build -o .tmp/wallet-server.exe ./cmd` |
| Available unit, HTTP, and database tests | PASS | Go 1.24, PostgreSQL 16; 42 service/database/router scenario functions plus the other package tests |
| Vet | PASS | `go vet ./...` |
| Lint | PASS | `golangci-lint run`, zero issues |
| Formatting | PASS | `golangci-lint fmt --diff`, empty output |
| Local cognitive complexity | PASS | gocognit reports no production service function at complexity 16 or higher |
| Patch whitespace | PASS | `git diff --check` |
| Race detector | PASS | `go test -race -count=1 ./...` passed on 2026-11-11 with CGO enabled, MSYS2 UCRT64 GCC 16.2.0, and the dedicated PostgreSQL `wallet_test` database. No races reported. |
| Make default target | PASS | GNU Make and MSYS2 Bash: formatting, lint, vet, and race-enabled tests all passed after clearing the test cache. |
| Remote CI and SonarQube | NOT RUN | No push/PR or remote scan performed. Existing CI setup limitations remain; `ci.yml` is unchanged. |

Coverage output from the final test run is a local artifact at `profiles/coverage.out`; it is not committed. Coverage is not used as a substitute for behavior checks.

## Live executable and documented setup

The original smoke verification applied the then-separate schema, UUID-default, and seed scripts with PostgreSQL 16 `psql` to a fresh isolated smoke database. These have since been consolidated into `000001_initial.up.sql` (schema with UUID defaults) and `setup.sql` (database creation and demo seeding). The documented commands use `-d` for the connection URL, avoiding Windows option-parsing errors. Schema version 2 was confirmed.

Built and launched the actual server executable in a hidden test console on a local port. A temporary Windows harness verified:

- `201` on the first transfer; a duplicate returned identical bytes and the replay header.
- `409` on conflicting payload reuse, `422` on insufficient funds, and `400` on an invalid amount.
- A signal-driven clean exit, followed by a new process replaying the previously committed result.
- A second signal-driven clean exit. Both exited with code 0 within the grace budget.

In-flight handler draining and forced closure after a deadline are also covered by the committed server tests. The smoke database ended with source balance 900, destination balance 100, two transfers (one processed/one failed), and two ledger entries. No application database was used.

## Failure and concurrency coverage

The real-router/database tests cover parallel identical keys, conflicting payloads, competing debits, opposite-direction transfers, exact terminal snapshots, lost HTTP response writes, and pending/terminal recovery through another connection pool. Financial checks verify append-only history, rejection of negative balances and incomplete processed pairs, and wallet/ledger reconciliation using NUMERIC sums, including total ledger turnover above int64.

Test-only transaction wrappers inject EOF after real reservation and processing COMMITs. These prove safe handling of a reported unknown outcome; they do not claim a literal TCP-drop matrix. Another test observes a blocked credit statement, terminates its own PostgreSQL backend, verifies rollback and generic HTTP 503, then retries the same key successfully. Full power-loss/application-process-kill testing is not claimed.

## Submission state

The solution branch is `solution/vaibhav-prakash`. The PR body is prepared in [PR_DESCRIPTION.md](./PR_DESCRIPTION.md), and AI disclosure/prompts are in [AI_USAGE.md](./AI_USAGE.md). Local commits are organized by domain/errors, persistence/workflow, HTTP/startup, behavioral tests, and documentation. No remote publication is included in this task. The preexisting.

`ci.yml` still installs a v1 linter while the repository configuration requires v2, and its installer/PATH command is malformed. The self-hosted runner, repository variables, PostgreSQL test service, and Sonar secrets also need repository-side setup. These limitations are documented rather than presented as passing remote checks.

## Race detector follow-up (2026-11-11)

All packages passed, including the service integration suite (15.691s); exit code 0. No race reports were emitted.

## HTTP pprof profiling (2026-11-11)

Collected and inspected all six requested profile types with `go tool pprof`: a 60-second CPU sample, live heap, cumulative allocations, goroutines, mutex contention, and blocking. The application has no pprof routes. A temporary local harness at `.tmp/profiling/main.go` registered loopback-only profiling endpoints and used the real HTTP handler, transfer service, GORM repositories, and PostgreSQL migrations in an isolated schema of `wallet_test`. The schema was dropped after collection. Production routing and application wallet data were unchanged. Mutex sampling (`runtime.SetMutexProfileFraction(1)`) and block sampling (`runtime.SetBlockProfileRate(1)`) were enabled for this run.

Four workers used eight wallets, alternating new transfers with exact idempotent replays. Over 66.04 seconds, 48,193 requests completed with zero unexpected status/transport failures; 24,095 were replays. This was an instrumented local diagnostic workload, not a capacity benchmark. The client/load generator shared the profiled process, all mutex/block events were sampled, and the harness used the test fixture's pgx simple protocol; results include those costs and do not isolate production-server overhead or PostgreSQL CPU.

| Profile | Observation |
| --- | --- |
| CPU | 60-second wall-time sample; 76.23 CPU seconds across cores. Windows syscall/cgo bridge accounted for 31.12% flat samples. Application cumulative stacks led through HTTP, GORM, and database operations; no isolated application CPU loop was identified. |
| Live heap | Sampled retained heap was about 3.50 MiB before load, 4.00 MiB during load, and 3.50 MiB after load and forced GC. These are sampled Go heap values, not process RSS. No retained-heap growth signal in this run. |
| Historical allocations | About 3,293.58 MiB cumulative sampled allocations; GORM statement cloning (15.75% flat) and clause construction (11.44% flat) led. This is allocation churn rather than live retained memory. It is a potential optimization target if latency/GC budgets warrant further measurement; no unsupported ORM or transaction rewrite was made. |
| Goroutines | Profile snapshots: 34 active, 7 during the post-load profile request; direct runtime counts returned from baseline 4 to 4 after collection. No goroutine leak signal in this run. |
| Mutex | 2.71 aggregate seconds of sampled contention, predominantly runtime locking; `sync.Mutex.Unlock` accounted for 0.09 seconds. No dominant application mutex bottleneck identified. |
| Block | 912.35 aggregate goroutine-seconds, dominated by selects/channel receives in HTTP, SQL transaction lifecycle, and pgx context watchers. Concurrent waiting accumulates beyond elapsed wall time; this alone is not a deadlock. All workers completed and the database pool reported zero in-use connections and zero pool waits afterward. |

No actionable correctness defect was demonstrated by these profiles.

Local artifacts (ignored by Git) are under `profiles/`: `cpu.pb.gz`, `heap-before.pb.gz`, `heap-live.pb.gz`, `heap-after.pb.gz`, `allocs.pb.gz`, `goroutine-active.pb.gz`, `goroutine-after.pb.gz`, `mutex.pb.gz`, `block.pb.gz`, the corresponding `*-top.txt` reports, and `workload.json`. The matching executable is `.tmp/profiling/profile-server.exe`. Reinspect from PowerShell:

```powershell
go tool pprof -top .tmp/profiling/profile-server.exe profiles/cpu.pb.gz
go tool pprof -top .tmp/profiling/profile-server.exe profiles/heap-live.pb.gz
go tool pprof -top -sample_index=alloc_space .tmp/profiling/profile-server.exe profiles/allocs.pb.gz
go tool pprof -top .tmp/profiling/profile-server.exe profiles/goroutine-active.pb.gz
go tool pprof -top .tmp/profiling/profile-server.exe profiles/mutex.pb.gz
go tool pprof -top .tmp/profiling/profile-server.exe profiles/block.pb.gz
```

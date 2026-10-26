# Verification report and final acceptance tests

**Reported by candidate:** On 2026-10-26 the complete Increment 5 PostgreSQL-backed regression suite passed on Windows without skipped tests. Increment 4's concurrency suite also passed three additional runs. These results were reported by the candidate, not reproduced by the artifact-generation environment.

## Verified in the candidate's local PostgreSQL run

| Suite | Behavior covered | Reported |
|---|---|---|
| `TestPostgresMigrationContract` | Schema creation; 11 checks of idempotency, FKs, constraints, immutable openings and ledger, reconciliation | Passed |
| `TestPostgresTransferService` | Successful transfer, insufficient funds, ledger state | Passed |
| `TestPostgresIdempotencyAndRollback` | Sequential success replay, conflicting payloads and nonexistent destination, FAILED replay after funding, late failure rollback, lost response | Passed |
| `TestPostgresConcurrentTransfers` | Real PostgreSQL-observed lock overlap; same-wallet competing debits, same-key callers, changed payload same key, opposite directions, unrelated wallets | Passed and repeated 3 times |
| `TestPostgresTransientTransactionRetries` | Injected `40P01`, `40001`, whole-transaction retries and bounded exhaustion | Passed |
| `TestPostgresHTTPTransfers` | HTTP 201 and replay, 409 conflicts and failed replay, 404 missing wallet, input errors, lost HTTP response, persisted accounting | Passed |
| Complete `go test ./...` regression | No skips reported | Passed |

## Added in final review: not yet verified against PostgreSQL

| Suite | Behavior | Status |
|---|---|---|
| `TestPostgresFinancialBoundaries` | Credit BIGINT overflow after debit; no partial persistence/key claim; later legitimate transfer and same-key retry | **Must run locally** |
| `TestPostgresForeignKeyErrorClassification` | Unexpected ledger FK errors aren't mislabeled as missing request wallets | **Must run final full Go suite** |
| `TestTransferRequestStructure` additional cases | Invalid UTF-8 rejected | Passed here (Go unit tests) |

These changes were added after the candidate's last reported full suite. **Do not carry the previous all-green result forward as verification for this final package.** Run `scripts/verify.ps1` and update the PR checklist from the actual local result.

## Local acceptance commands (Windows PowerShell)

```powershell
$env:PGPASSWORD = 'YOUR_PASSWORD'
$env:WALLET_TEST_DATABASE_URL = 'postgres://postgres@localhost:5432/wallet_transfer_test?sslmode=disable'
go mod download
go mod verify
.\scripts\check-format.ps1
go vet ./...
.\scripts\verify.ps1
# Optional repeat run:
go test ./internal/service -run '^TestPostgresConcurrentTransfers$' -count=3 -v -timeout 8m
```

The fail-closed script checks for nonzero exit, any `--- SKIP:` output, and the explicit suite PASS markers. Integration tests allocate random test schemas; the database must be disposable and permit creating/dropping schemas. If any command fails, record its exact output and correct the code before marking the PR checklist complete.

## Not claimed as verified

- Real connection drop **during** PostgreSQL `COMMIT` with ambiguity about the outcome.
- Full production failure modes under multi-node deployment, network partitions, backups/restore, or massive wallet contention.
- Cross-table ledger completeness enforced exclusively through database triggers (not implemented).
- Security, authentication, TLS, currency precision beyond a single integer unit.
- `go test -race ./...` on the final package; run separately if the Windows compiler/toolchain supports it.

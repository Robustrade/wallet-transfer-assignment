# Transfer service

`transfer.go` owns request validation, the synchronous idempotent transfer workflow, state decisions, ordered wallet-lock coordination, and bounded retries of PostgreSQL-confirmed transaction aborts. The service depends on a narrow `TransferStore` interface and does not execute SQL directly.

PostgreSQL integration tests in this directory cover normal and failed transfers, replay, late rollback, concurrent operations, transient retries, HTTP-to-database behavior, and credit overflow. They require a real disposable PostgreSQL test database. Run `.\scripts\verify.ps1` from the repository root; see `docs/TEST_PLAN.md` for verification status.

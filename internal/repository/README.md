# PostgreSQL repository

`postgres.go` implements the service transaction contract using `database/sql` and `github.com/lib/pq`. It owns `READ COMMITTED` transaction boundaries, durable unique idempotency-key claims and committed-result reads, wallet row locks with `FOR NO KEY UPDATE`, guarded debit, credit, paired ledger entries, and guarded terminal status updates.

It translates PostgreSQL-confirmed deadlock/serialization aborts into a retryable transaction error and maps only missing source/destination FKs on a newly claimed transfer to `ErrWalletNotFound`. Unexpected FKs remain internal errors. There is no automatic retry for an uncertain commit outcome; clients should retry the original idempotency key. See `docs/DESIGN.md`.

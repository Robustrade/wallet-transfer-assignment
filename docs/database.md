# Database setup

The service uses PostgreSQL. Configure the connection using environment variables:

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `DATABASE_URL` | Yes | — | PostgreSQL connection URL, including host and database name |
| `DB_MAX_OPEN_CONNS` | No | `25` | Maximum open connections |
| `DB_MAX_IDLE_CONNS` | No | `5` | Maximum idle connections; cannot exceed the open limit |
| `DB_CONN_MAX_LIFETIME` | No | `5m` | Maximum lifetime of a pooled connection (Go duration syntax) |

Example:

```sh
export DATABASE_URL='postgres://wallet:password@localhost:5432/wallets?sslmode=disable'
export DB_MAX_OPEN_CONNS=25
export DB_MAX_IDLE_CONNS=5
export DB_CONN_MAX_LIFETIME=5m
```

The database package validates these settings and provides an `Open` helper that configures and pings a `database/sql` connection pool. The application composition layer registers the chosen PostgreSQL driver before calling it. Repositories depend on an interface implemented by both `*sql.DB` and `*sql.Tx`.

## Migrations

The application runs the embedded `*.up.sql` migrations automatically at startup through `migrations.Apply`. It applies migrations in numeric order and records each successful migration in `schema_migrations` in the same transaction as the schema changes. For a fresh database, start the service with a valid `DATABASE_URL`; do not run the up SQL file manually, as that would create the tables without recording migration history.

To roll back the initial schema manually, run the matching down migration:

```sh
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f migrations/000001_create_wallet_transfer_tables.down.sql
```

The down migration drops the application tables and removes version 1 from `schema_migrations` atomically. On the next application startup, `migrations.Apply` will therefore apply the initial migration again. Run this rollback only when the service is stopped and you intend to remove the schema and its data.

The up migration creates wallets, transfers, ledger entries, and idempotency records. It includes database constraints for non-negative wallet balances, positive transfer/ledger amounts, valid transfer and ledger states, distinct transfer wallets, and unique debit/credit entry types per transfer.

Wallet, transfer, and ledger repositories can all be constructed with the same `*sql.Tx`. The service layer must use that transaction for the complete transfer operation so wallet balance changes, the terminal transfer status, and both ledger entries commit or roll back together.

The transfer repository only permits `PENDING -> PROCESSED` and `PENDING -> FAILED`. Ledger entries are append-only through the repository API; the database's unique `(transfer_id, entry_type)` constraint prevents duplicate debit or credit entries for a transfer.

The service transaction must still guarantee that a processed transfer has exactly one debit and one credit, that their amounts match, and that their wallet IDs match the transfer endpoints; ordinary row constraints cannot enforce those cross-row invariants.

The idempotency response columns may be temporarily null while a request is being processed inside a transaction. The check constraint requires the response status and body to be either both null or both present. The application must populate both before committing, so an incomplete record is never committed.

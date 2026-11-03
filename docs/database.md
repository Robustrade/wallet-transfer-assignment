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

The database package validates these settings and provides an `Open` helper that configures and pings a `database/sql` connection pool. The application must register its PostgreSQL driver before calling it; driver selection and dependency wiring will be done with the repository layer.

## Migrations

Apply migrations in numeric order using a PostgreSQL client. For a fresh database:

```sh
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f migrations/000001_create_wallet_transfer_tables.up.sql
```

To roll back this initial schema:

```sh
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f migrations/000001_create_wallet_transfer_tables.down.sql
```

The up migration creates wallets, transfers, ledger entries, and idempotency records. It includes database constraints for non-negative wallet balances, positive transfer/ledger amounts, valid transfer and ledger states, distinct transfer wallets, and unique debit/credit entry types per transfer.

The unique ledger constraint prevents duplicate entries of the same type for a transfer. The service transaction must still guarantee that a processed transfer has both entries, that their amounts match, and that their wallet IDs match the transfer endpoints; ordinary row constraints cannot enforce that cross-row invariant by themselves.

The idempotency response columns may be temporarily null while a request is being processed inside a transaction. The check constraint requires the response status and body to be either both null or both present. The application must populate both before committing, so an incomplete record is never committed.

# Database migrations

Schema changes live in numbered `*.up.sql` / `*.down.sql` pairs and are applied
with [golang-migrate](https://github.com/golang-migrate/migrate). Seed/fixture
data is intentionally **not** a migration (see `seed.sql`).

## Install the migrate CLI

```
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
```

## Start the database

```
docker compose up -d postgres
```

## Apply / roll back schema

```
# up (apply all pending)
migrate -source file://./migrations \
  -database postgresql://myuser:mypassword@localhost:5432/mydb?sslmode=disable up

# down (reverse the last migration)
migrate -source file://./migrations \
  -database postgresql://myuser:mypassword@localhost:5432/mydb?sslmode=disable down 1
```

Down migrations are data-independent: they drop tables children-first, so a
teardown succeeds even when transfers and ledger rows exist.

## Seed dev/test wallets (optional, not a migration)

```
docker exec -i postgres_db psql -U myuser -d mydb < migrations/seed.sql
```

Seed data is kept out of the numbered migrations on purpose: a schema migration
must reverse cleanly regardless of runtime data, whereas deleting seed wallets
can be blocked by transfers that reference them.

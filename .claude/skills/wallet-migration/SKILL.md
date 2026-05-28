---
name: wallet-migration
description: Use this skill whenever a change to the database schema is needed in this wallet-transfer repo — adding or altering tables, columns, indexes, CHECK constraints, FOREIGN KEY constraints, UNIQUE constraints, or enum-style domains. Trigger when the user asks to "add a column", "create a table", "add an index", "change the schema", "add a constraint", "drop a column", "migrate the database", or when an implementation step is blocked because the schema is missing a field. Do NOT trigger for app-code-only changes that do not touch SQL DDL, or for SELECT/INSERT/UPDATE/DELETE work which is covered by postgres-tx-safety.
---

# wallet-migration

Migrations in this repo are **forward-only and reversible**. Once a
migration file is merged it is never edited. New schema changes go in a
new numbered file.

Read [AGENTS.md](../../../AGENTS.md) §S-12 (Migrations) before authoring
the change.

---

## File layout — there are TWO copies that must stay in sync

```
migrations/                   <- source of truth (developer-facing)
internal/db/migrations/       <- embedded into the binary via go:embed
```

Both directories must contain identical files. The `internal/db/db.go`
file embeds the second copy:

```go
//go:embed migrations/*.sql
var migrationsFS embed.FS
```

Forgetting to update the embedded copy is the most common mistake here —
the source-of-truth files apply when you run the migrate CLI by hand, but
the running binary uses only the embedded copy.

---

## Naming convention

`NNNN_short_description.{up,down}.sql`

- `NNNN` — zero-padded, monotonically increasing. Look at the highest
  number currently in `migrations/` and add 1.
- `short_description` — snake_case, ≤ 4 words.

Examples:
- `0002_add_currency_to_wallets.up.sql`
- `0002_add_currency_to_wallets.down.sql`
- `0003_index_transfers_by_state.up.sql`

---

## Authoring workflow

1. **Pick the number.**
   ```sh
   ls migrations/ | sort | tail -2
   ```
   Take the next integer.

2. **Write the up migration in `migrations/NNNN_xxx.up.sql`.**

   Template:
   ```sql
   BEGIN;

   -- 1. Schema change here (see "Patterns" below).

   COMMIT;
   ```

3. **Write the down migration in `migrations/NNNN_xxx.down.sql`.**

   It should fully reverse the up. If a reversal would lose data
   (e.g. dropping a NOT NULL column), state that explicitly in a comment
   and confirm with the user before writing it.

4. **Copy both files into `internal/db/migrations/`.**

   ```sh
   cp migrations/NNNN_xxx.up.sql migrations/NNNN_xxx.down.sql internal/db/migrations/
   ```

5. **Verify both copies are byte-identical.**

   ```sh
   diff migrations/NNNN_xxx.up.sql internal/db/migrations/NNNN_xxx.up.sql
   diff migrations/NNNN_xxx.down.sql internal/db/migrations/NNNN_xxx.down.sql
   ```

6. **Apply locally** to confirm the migration is syntactically valid and
   reversible:

   ```sh
   make db-up         # or ensure postgres is running
   make run           # applies pending migrations at startup
   # if there is a CLI-based path, run it; otherwise restarting the
   # server is enough.
   ```

   For an integration-test check:
   ```sh
   make db-up        # docker-compose Postgres
   make test-int     # applies migrations end to end against it
   ```

---

## Patterns

### Adding a column

```sql
BEGIN;

ALTER TABLE wallets
    ADD COLUMN currency TEXT NOT NULL DEFAULT 'USD'
    CHECK (currency ~ '^[A-Z]{3}$');

COMMIT;
```

Down:
```sql
BEGIN;
ALTER TABLE wallets DROP COLUMN currency;
COMMIT;
```

If the table is large, `ADD COLUMN ... NOT NULL DEFAULT` blocks the
table on PostgreSQL versions older than 11. PostgreSQL 16 (what we use)
handles this efficiently; you can keep the simple form.

### Adding a CHECK constraint to an existing column

```sql
BEGIN;
ALTER TABLE transfers
    ADD CONSTRAINT transfers_amount_positive CHECK (amount > 0) NOT VALID;
ALTER TABLE transfers VALIDATE CONSTRAINT transfers_amount_positive;
COMMIT;
```

Why `NOT VALID` + `VALIDATE`: `NOT VALID` lets the constraint be added
without a full table scan, then `VALIDATE` does the scan without
blocking writes. For a small table this is overkill but harmless.

### Adding a UNIQUE constraint

Prefer `CREATE UNIQUE INDEX` over `ALTER TABLE ... ADD CONSTRAINT UNIQUE`
when the table may be large — the former can be made `CONCURRENTLY` for
zero-downtime, the latter cannot.

```sql
CREATE UNIQUE INDEX CONCURRENTLY transfers_idempotency_key_uidx
    ON transfers (idempotency_key);
```

NOTE: `CREATE INDEX CONCURRENTLY` cannot run inside a `BEGIN/COMMIT`
block. For that one statement, omit the wrapper.

### Adding an index

```sql
BEGIN;
CREATE INDEX transfers_state_idx ON transfers (state);
COMMIT;
```

Or concurrently for large tables (no BEGIN wrapper):
```sql
CREATE INDEX CONCURRENTLY transfers_state_idx ON transfers (state);
```

### Adding a new table

```sql
BEGIN;

CREATE TABLE refunds (
    id              UUID        PRIMARY KEY,
    transfer_id     UUID        NOT NULL REFERENCES transfers(id),
    amount          BIGINT      NOT NULL CHECK (amount > 0),
    state           TEXT        NOT NULL CHECK (state IN ('PENDING','PROCESSED','FAILED')),
    failure_reason  TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX refunds_transfer_idx ON refunds (transfer_id);

COMMIT;
```

Down:
```sql
BEGIN;
DROP TABLE IF EXISTS refunds;
COMMIT;
```

---

## House rules baked into every migration

- [ ] `BIGINT` for monetary amounts (never `NUMERIC` or `INTEGER` without
      a reason).
- [ ] `TIMESTAMPTZ` for times (never `TIMESTAMP` without TZ).
- [ ] Every CHECK / UNIQUE / FK that exists in app validation also exists
      at the DB layer. Defence in depth.
- [ ] Monetary checks: `amount > 0`, `balance >= 0`.
- [ ] String enums: `CHECK (col IN ('A','B','C'))` — prefer over native
      ENUMs because they are easier to evolve.
- [ ] Timestamps default to `now()` and update via app code or a trigger
      (we update in app code today).
- [ ] FK columns are indexed (Postgres does NOT auto-index FK targets'
      referrers).

---

## After the migration is in

1. **Update the repository layer** — new columns probably need to be
   scanned in `scanWallet` / `scanTransfer` / etc.
2. **Update the domain model** — add the new field.
3. **Update tests** — extend integration tests to cover the new field.
4. **Update the README ER diagram** if the change is structural (new
   table or new key relationship).

---

## Common mistakes

| Mistake | What happens | How to avoid |
|---|---|---|
| Editing a merged migration file | Different schemas across environments | Add a new file |
| Forgetting to copy into `internal/db/migrations/` | Binary applies old schema | Step 4 + 5 of the workflow |
| `ENUM` type instead of `CHECK (col IN (...))` | Painful to add/remove values | Use CHECK |
| `NOT NULL` without `DEFAULT` on a populated table | Migration fails on existing rows | Add DEFAULT (and consider backfill) |
| Missing index on FK column | Slow JOINs and DELETE cascades | Add `CREATE INDEX <table>_<col>_idx` |
| Skipping the down migration | Cannot roll forward safely | Always pair up + down |

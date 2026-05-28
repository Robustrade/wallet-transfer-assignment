---
name: postgres-tx-safety
description: Use this skill any time work in this wallet-transfer repo involves SQL queries, opening or extending a database transaction, locking wallet rows, or modifying balance/idempotency logic. Trigger especially when editing files under internal/repository/, internal/service/, or migrations/, or when the user asks to "add an endpoint that touches a wallet", "update balance", "add a query", "fix a race", or anything involving concurrency on the wallets/transfers/ledger_entries/idempotency_records tables. Do NOT trigger for pure handler/JSON work that does not reach the database, for domain-only changes, or for documentation-only edits.
---

# postgres-tx-safety

This is a financial system. Read [AGENTS.md](../../../AGENTS.md) §1
(SECURITY) and §3 (Database conventions) first. The rules below are the
**operational templates** for following those rules — they do not replace
the rules.

If anything in this skill conflicts with `AGENTS.md`, `AGENTS.md` wins.

---

## The four hard rules

1. **Parameterize every query.** No `fmt.Sprintf` into SQL, ever. (S-1)
2. **Mutations live inside `RunInTx`.** A read-then-write outside a tx is
   a TOCTOU race. (S-3)
3. **Lock wallets in id-ascending order.** Use `orderedPair`. Direction
   of the transfer is irrelevant; lock order is determined by id. (S-4)
4. **Money is `int64` minor units.** Never `float64`. (S-2)

---

## Templates

### Template A — single-table parameterized SQL

```go
const q = `SELECT id, balance, created_at, updated_at FROM wallets WHERE id = $1`
row := exec.QueryRowContext(ctx, q, id)
// scan into a typed struct
```

Rules:
- Query is a `const string`, not built with `+` or `fmt.Sprintf`.
- All caller input is a `$N` placeholder.
- `ctx` is the first arg; `exec` (Executor) is the second.

### Template B — mutating two wallet rows safely

```go
err := s.txm.RunInTx(ctx, func(exec repository.Executor) error {
    firstID, secondID := orderedPair(srcID, dstID)
    if _, err := s.wallets.LockForUpdate(ctx, exec, firstID); err != nil {
        return err
    }
    if _, err := s.wallets.LockForUpdate(ctx, exec, secondID); err != nil {
        return err
    }
    src, err := s.wallets.Get(ctx, exec, srcID)
    if err != nil { return err }
    dst, err := s.wallets.Get(ctx, exec, dstID)
    if err != nil { return err }

    // 1. validate invariants (e.g. src.Balance >= amount)
    // 2. mutate (balance updates + ledger entries + state writes)
    return nil
})
```

Checklist for any code matching this shape:
- [ ] Locks acquired BEFORE balance is read.
- [ ] Locks acquired via `orderedPair`, not in request order.
- [ ] Every mutation inside the closure goes through `exec`, not `s.db`.
- [ ] The closure returns the error directly; the deferred rollback in
      `RunInTx` handles cleanup.

### Template C — idempotency on a money-moving endpoint

Money-moving endpoints are required to be idempotent (S-9). Use the
existing pattern verbatim:

```go
// Fast path - replay of an already-committed request.
if t, err := s.replayIfExists(ctx, s.exec, req.IdempotencyKey, hash); err != nil {
    return nil, err
} else if t != nil {
    return t, nil
}

// Slow path - run inside tx; PK violation on idempotency_records is the serialization.
err := s.txm.RunInTx(ctx, func(exec repository.Executor) error {
    // 1. lock & validate
    // 2. INSERT the side-effect row (e.g. transfer)
    // 3. INSERT idempotency_records (key, hash, side_effect_id)
    //    -> returns repository.ErrIdempotencyExists on conflict
    return nil
})
if errors.Is(err, repository.ErrIdempotencyExists) {
    // Lost a race; the winner has now committed. Replay.
    return s.replayIfExists(ctx, s.exec, req.IdempotencyKey, hash)
}
```

The idempotency row and the side-effect row MUST commit in the same tx
so they cannot diverge.

### Template D — `ORDER BY` / `LIMIT` from caller input

NEVER interpolate. Use an allowlist switch:

```go
var order string
switch sortKey {
case "created_at":
    order = "created_at"
case "amount":
    order = "amount"
default:
    return nil, fmt.Errorf("invalid sort key")
}
const q = `SELECT ... FROM transfers ORDER BY ` + order + ` LIMIT $1`
```

The `order` value comes from the switch, never directly from the caller.
`LIMIT` itself is still a `$N` placeholder.

### Template E — `LIKE` patterns from caller input

```go
// Escape %, _, \ in caller-supplied search text.
safe := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(input)
const q = `SELECT ... WHERE name LIKE $1 ESCAPE '\'`
rows, err := exec.QueryContext(ctx, q, "%"+safe+"%")
```

---

## Anti-patterns that get auto-rejected at review

| Pattern | Reason | Fix |
|---|---|---|
| `db.Exec("UPDATE … WHERE id='" + id + "'")` | SQL injection | parameterize with `$N` |
| `db.Exec("UPDATE wallets SET balance=" + ... )` | TOCTOU + injection | `RunInTx` + `FOR UPDATE` + `$N` |
| `SELECT balance FROM wallets` then UPDATE outside tx | TOCTOU race | inside `RunInTx`, `FOR UPDATE` |
| Locking source then destination by *direction* | Deadlock under counter-transfers | `orderedPair` |
| Returning `pgErr.Error()` to the HTTP client | Leaks schema and constraint names | map to sentinel; let `writeError` translate |
| New money-moving endpoint without `IdempotencyKey` | Duplicate-charge on retry | Template C |
| `float64` for amount, fee, balance, anything monetary | Precision loss | `int64` minor units |
| `repo.Get(ctx, db, ...)` then `repo.Update(ctx, db, ...)` with `db` not `exec` inside a tx callback | Bypasses tx; updates go to a different connection | thread `exec` everywhere |

---

## When the model is a unique-violation error

Check the SQLSTATE explicitly, do not match on `err.Error()` strings.

```go
var pgErr *pgconn.PgError
if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation { // "23505"
    return repository.ErrIdempotencyExists
}
```

Helper constants for common SQLSTATEs:
- `23505` — unique violation
- `23503` — foreign key violation
- `23514` — check constraint violation
- `40001` — serialization failure (retriable)
- `40P01` — deadlock detected (retriable, but you should fix the lock order rather than retry)

---

## Verification after editing

Before declaring done:

```sh
go vet ./... && go vet -tags=integration ./...
go test -race ./internal/domain/...
go test -race -tags=integration -count=1 -timeout=300s ./internal/service/... ./internal/handler/...
```

And run `/secure-review` for a final security pass.

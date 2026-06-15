# Pull Request Notes

## Summary

Implemented a Go wallet transfer service with SQLite persistence. The solution supports wallet setup, idempotent wallet-to-wallet transfers, stored balances, transfer state transitions, and double-entry ledger rows.

## AI Disclosure

I used OpenAI Codex as an AI pair-programming assistant for this submission. Codex helped inspect the assignment and repository instructions, implement the Go + SQLite service, add tests, run verification commands, and prepare documentation. I verified the generated implementation locally with format, test, race, vet, and lint commands.

The detailed AI disclosure, prompt log, and transcript fallback are in `docs/ai-disclosure.md`.

## Schema Design

The schema includes:

- `wallets`: wallet ID, stored non-negative balance, and timestamps.
- `transfers`: transfer ID, unique idempotency key, normalized request hash, wallet IDs, amount, state, error code, and timestamps.
- `ledger_entries`: debit/credit rows linked to transfers and wallets, with uniqueness on `(transfer_id, entry_type)`.

Transfer wallet IDs are stored as request snapshots so missing-wallet failures can be durably replayed. Ledger entries still use foreign keys so posted accounting rows reference valid wallets and transfers.

## Idempotency Strategy

`POST /transfers` hashes the normalized transfer payload and stores it with a unique idempotency key. A duplicate request with the same key and same payload returns the original persisted transfer result and ledger rows. Reusing the key with different payload values returns `409 idempotency_key_conflict`.

## Concurrency Strategy

Each transfer runs inside a SQLite `BEGIN IMMEDIATE` transaction. The source debit is guarded with:

```sql
UPDATE wallets
SET balance = balance - ?
WHERE id = ? AND balance >= ?;
```

That keeps the balance check and debit atomic, preventing concurrent debits from overspending the same wallet. Transfer state updates are guarded so only `PENDING` transfers move to terminal states.

## How to Run

```bash
go run ./cmd/server
```

Optional environment variables:

- `PORT`, default `8080`
- `DATABASE_DSN`, default `file:wallet.db?_busy_timeout=5000&_foreign_keys=on`

## How to Test

```bash
test -z "$(gofmt -l .)"
go test ./... -race -cover
go vet ./...
go run github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8 run ./...
```

## Tradeoffs / Assumptions

- SQLite was chosen because the assignment accepts it and it keeps the submission self-contained.
- Balances are stored rather than derived from the ledger so transfer execution can use guarded balance updates.
- Missing wallets and insufficient funds are durable `FAILED` transfer outcomes and can be replayed by idempotency key.

## Checklist

- [x] Tests pass
- [x] Lint passes
- [x] Format check passes
- [x] README or notes updated
- [x] PR description explains schema, idempotency, and concurrency

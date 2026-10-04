# Pull request notes

Paste this into the pull request description. There is no git remote in this workspace, so the pull request itself was not opened.

## Summary

Wallet-to-wallet transfers with a double-entry ledger, stored balances, and idempotent `POST /transfers`.

- One database transaction claims the idempotency key, locks both wallets in id order, moves the balance, and writes the debit and credit.
- A repeated key returns the original transfer. A repeated key with a different payload is a conflict and does not post again.
- Concurrent debits cannot overspend. PostgreSQL rejects a negative balance, a committed `PENDING` transfer, and an unbalanced ledger.

## AI disclosure

1. Tool: Cursor, using the Grok 4.7 coding agent.
2. How it was used: the assignment and evaluation guide were read first, the schema and transaction design were written down, and the agent implemented the service from that design. The concurrency and idempotency behavior was then checked with PostgreSQL tests and a live process, not accepted only because it compiled.
3. Prompts: the session started from the instruction to implement `assignment.md` and `evaluation_guide.md` in Go with hexagonal architecture, PostgreSQL, integer money, tests, and docs. Follow-up direction was to keep business rules out of handlers and repositories and to prove duplicate keys and concurrent debits. `docs/ai-disclosure.md` has the same note. Export the Cursor transcript from the editor session if the full log is required.

## Schema Design

- `wallets`: text id, `initial_balance`, `balance`, both `bigint` and `>= 0`.
- `transfers`: uuid id, unique `idempotency_key`, source and destination foreign keys, amount `> 0`, distinct wallets, status `PENDING | PROCESSED | FAILED`, failure reason required only for `FAILED`.
- `ledger_entries`: one debit and one credit per transfer (`UNIQUE (transfer_id, entry_type)`), positive amounts, foreign keys to the wallet and the transfer.
- `idempotency_records`: key primary key, request hash, unique nullable `transfer_id`.
- Deferred triggers reject a commit that is still `PENDING`, a processed transfer whose ledger is not a balanced pair on the right wallets, a failed transfer that has ledger rows, or an idempotency row with no transfer.

`initial_balance` is opening funding and is not a ledger row. Reconciliation is `balance = initial_balance + credits - debits`.

## Idempotency Strategy

The key and a SHA-256 of `fromWalletId`, `toWalletId`, and `amount` are inserted with `ON CONFLICT DO NOTHING`. A concurrent insert of the same key waits on the unique index. The winner performs the transfer and stores `transfer_id` before commit. The loser loads that transfer and returns it. A hash mismatch returns 409 and writes nothing. Validation failures and unknown wallets roll back, so the key is not consumed. Insufficient funds commits `FAILED` and the same key returns that failure. The HTTP body is rendered from the transfer row, so a retry matches the original id, status, and timestamps. `transfers.idempotency_key` is also unique.

## Concurrency Strategy

`READ COMMITTED` transaction. Idempotency key is claimed first. Wallet rows are then locked with `SELECT ... FOR UPDATE` in ascending id order, which avoids the A-to-B versus B-to-A deadlock. The debit is `UPDATE ... WHERE balance >= amount`, with `CHECK (balance >= 0)` behind it. Lock and statement timeouts are set on the transaction so a stuck waiter becomes a retryable 503 instead of hanging. The retry is safe because of the idempotency key.

## How to Run

```bash
export WALLET_DB_PASSWORD=wallet
make run
```

Or `docker compose up --build`. Do not put the database password in `config.yaml`.

## How to Test

```bash
make test
make test-race
make vet
make lint
```

## Tradeoffs / Assumptions

- `idempotencyKey` is required and global. There is no auth in the assignment.
- Amounts are integer minor units of one currency. Fractional JSON is rejected.
- `PENDING` is used inside the transaction and is not visible after commit. Crashes roll back. A lost response is handled by retrying the same key.
- Insufficient funds consumes the key. A later retry needs a new key.
- Wallet creation with `initialBalance` is administrative funding, not a ledger transfer.
- Metrics are not included.

## Checklist

- [x] Tests pass
- [x] Lint passes
- [x] Format check passes
- [x] README or notes updated
- [x] PR description explains schema, idempotency, and concurrency

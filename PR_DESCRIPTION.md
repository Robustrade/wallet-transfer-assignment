## Summary

Implements `POST /transfers` with stored wallet balances, durable idempotency, a double-entry ledger, guarded transfer states, and concurrency-safe debits. A PostgreSQL deferred constraint trigger now prevents any transaction from committing an invalid ledger state.

## AI disclosure

I used Codex, an AI coding agent, to review the repository, reason about the transaction and ledger invariants, implement the approved migration and tests, and run the verification suite. I reviewed the generated changes and chose the final approach.

Prompts used in this session:

- `review this repo and Assignments.md is the problem satatement and expectations`
- `fix P1 — Ledger invariants are not enforced by the database.`
- `approve`

The full interaction transcript is available in the coding-agent session associated with this submission.

## Schema Design

- `wallets` stores a non-negative materialized balance.
- `transfers` stores source/destination wallet IDs, amount, `PENDING|PROCESSED|FAILED` state, failure reason, and timestamps.
- `ledger_entries` stores debit and credit postings with foreign keys to wallets and transfers. `(transfer_id, entry_type)` prevents duplicate entries of either type.
- `idempotency_records` stores the idempotency key, a canonical request hash, and the original transfer.

The schema also enforces positive amounts, valid enum values, distinct transfer wallets, foreign keys, and useful history indexes.

Change set 3 adds deferred PostgreSQL constraint triggers. At commit, a `PROCESSED` transfer must have exactly one debit from its source wallet and one credit to its destination wallet, both matching the transfer amount. `PENDING` and `FAILED` transfers may not have ledger postings.

## Idempotency Strategy

Each request takes a PostgreSQL transaction-scoped advisory lock derived from its idempotency key, then reads the durable idempotency record.

- A repeated key with the same canonical payload returns the original transfer without side effects.
- A repeated key with a different payload returns HTTP 409.
- The idempotency record, transfer, balance updates, ledger rows, and terminal state are committed in one transaction. Unexpected failures roll back all of them.

## Concurrency Strategy

Wallet rows are loaded with `SELECT ... FOR UPDATE` and acquired in lexical wallet-ID order. This serializes competing debits while avoiding opposite-direction lock ordering. A transfer that waits for a concurrent debit observes the latest committed balance and fails safely if insufficient funds remain.

## How to Run

1. Start PostgreSQL: `docker compose up -d`
2. Start the service: `mvn spring-boot:run`
3. Send `POST http://localhost:9090/transfers` with `idempotencyKey`, `fromWalletId`, `toWalletId`, and `amount`.

## How to Test

- Run `mvn clean test` with Docker available for Testcontainers.
- The suite covers successful transfers, idempotent replay/conflicts, insufficient funds, concurrent debit safety, concurrent duplicate requests, state transitions, and database-enforced ledger invariant violations.

## Tradeoffs / Assumptions

- Stored balances make reads cheap; atomic transactions keep them consistent with the ledger.
- Pessimistic locking favors correctness and clear behavior over maximum throughput for a hot wallet.
- Advisory locks and deferred triggers are PostgreSQL-specific, which is acceptable because PostgreSQL is the preferred assignment database.
- The API has no currency field, so every wallet is assumed to use the same unit.

## Checklist

- [x] Tests pass (`mvn clean test`: 31 tests)
- [ ] Lint passes (no repository lint command is configured)
- [ ] Format check passes (no repository format command is configured)
- [ ] README or notes updated (docs/wallet-transfer-readme.md)
- [x] PR description explains schema, idempotency, and concurrency


## Notes
Visit `docs/wallet-transfer-readme.md` for a more detailed explanation.
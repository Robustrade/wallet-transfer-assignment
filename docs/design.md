# Wallet Transfer Service — Design Note

## Problem statement

Move money between two wallets over an HTTP API such that a client may retry a
request any number of times, several API instances may serve those retries
concurrently, and the recorded money is always correct: never created, never
destroyed, never half moved.

## Contract

### `POST /transfers`

```json
{ "idempotencyKey": "abc123", "fromWalletId": "wallet_1", "toWalletId": "wallet_2", "amount": 100 }
```

`amount` is an integer in minor units (cents). Floating point is never used for
money anywhere in the system.

| Outcome | Status | Body |
| --- | --- | --- |
| Settled | `201` | transfer with `status: PROCESSED` and its two ledger entries |
| Replay of a settled transfer | `201` + `Idempotent-Replay: true` | byte-identical to the original response |
| Source wallet could not fund it | `422` | `INSUFFICIENT_FUNDS` with the transfer id |
| Key reused with a different payload | `409` | `IDEMPOTENCY_KEY_CONFLICT` |
| Key claimed but not resolvable | `409` | `CONCURRENT_REQUEST`, safe to retry |
| Unknown wallet | `404` | `WALLET_NOT_FOUND` |
| Invalid amount, same wallet, missing field | `400` | `INVALID_REQUEST` |

Supporting endpoints: `POST /wallets`, `GET /wallets/{id}`,
`GET /transfers/{id}`, `GET /healthz`.

### Side effects

A settled transfer writes, atomically: one `transfers` row in `PROCESSED`, two
`ledger_entries` rows, two `wallets` balance updates and one
`idempotency_records` row. Nothing else in the system observes a partial
version of that set.

## Schema

```
wallets(id PK, balance CHECK >= 0, created_at, updated_at)

transfers(id PK, from_wallet_id FK, to_wallet_id FK, amount CHECK > 0,
          status CHECK IN (PENDING, PROCESSED, FAILED), failure_reason,
          CHECK from_wallet_id <> to_wallet_id,
          CHECK failure_reason present exactly when status = FAILED)

ledger_entries(id PK, transfer_id FK, wallet_id FK,
               entry_type CHECK IN (DEBIT, CREDIT), amount CHECK > 0,
               UNIQUE (transfer_id, entry_type))

idempotency_records(key PK, request_hash, transfer_id FK, created_at)
```

The constraints are chosen so that the invariants survive a bug in the service
or a hand-written `UPDATE`:

- `balance >= 0` makes an overdraft impossible. Removing the row locks from the
  transfer path and rerunning the concurrency tests shows this constraint
  rejecting the debit that the race would otherwise have allowed.
- `UNIQUE (transfer_id, entry_type)` caps a transfer at one debit and one
  credit, so a replayed settlement cannot double post.
- The foreign keys mean a ledger entry cannot exist without its transfer, and a
  transfer cannot reference a wallet that does not exist.
- `idempotency_records.key` as the primary key is the deduplication mechanism
  itself, not a cache of one.

Indexes cover the access paths the API uses: ledger entries by wallet and by
transfer (via the unique constraint), transfers by either wallet.

## Transfer workflow

The request is handled in two committed steps.

**Reserve.** In one transaction: lock both wallets, insert the transfer as
`PENDING`, insert the idempotency record. Because the key and the transfer
commit together, the key exists if and only if its transfer does.

**Settle.** In one transaction: lock the transfer row, re-read both wallets
under lock, then either

- post the debit and credit, apply both balance updates and move the transfer
  to `PROCESSED`; or
- move the transfer to `FAILED` with `INSUFFICIENT_FUNDS`, writing no entries
  and touching no balance.

Both branches update the status with `WHERE id = ? AND status = 'PENDING'`, so
the state transition is guarded rather than assumed.

The ledger entries and the `PROCESSED` status change are in the same
transaction. A transfer therefore cannot be reported as successful unless its
entries committed — the failure mode the assignment calls out is excluded by
the transaction boundary, not by application care.

## Concurrency

Two rules, applied everywhere:

1. **Every transaction locks wallets with `SELECT ... FOR UPDATE` in ascending
   wallet id order.** This is what makes `alice -> bob` and `bob -> alice` safe
   to run at the same time. Locking in request order instead deadlocks:
   removing the sort makes the opposing-transfer test fail with PostgreSQL
   `SQLSTATE 40P01`.

   Reserve locks the wallets even though it only needs to know they exist,
   because inserting the transfer takes foreign key locks on those same two
   rows in column order. Taking them ourselves in sorted order first keeps the
   whole service on one ordering.

2. **Settlement locks the transfer row before any wallet, and nothing ever
   takes a wallet lock before a transfer lock.** Two attempts to settle the
   same transfer therefore queue instead of both seeing `PENDING`. The lock
   graph is `transfer -> wallets -> idempotency key`, which is acyclic, so
   there is no ordering that can deadlock.

Balance arithmetic is done by the database (`SET balance = balance + ?`) rather
than read into the process and written back, so even an unlocked path could not
lose an update.

The default isolation level is enough: every row the transaction intends to
write is locked before it is read, so there is no read-then-write window.
`SERIALIZABLE` would add retry-on-abort handling for no additional guarantee
here.

There is no application-level mutex anywhere. Correctness lives entirely in the
database, so it holds across any number of API instances.

## Idempotency

The idempotency key is the primary key of `idempotency_records`, written inside
the same transaction as the transfer it refers to.

- **Same request twice.** The second insert violates the primary key. The
  service rolls back — discarding the transfer row it had just inserted —
  re-reads the record, and returns the original transfer.
- **Same key concurrently.** The second transaction blocks on the index until
  the first commits, then takes the path above. Both callers get the same
  transfer id and money moves once.
- **Response lost after commit.** The retry is an ordinary replay.
- **Key reused with a different payload.** The stored `request_hash`, a SHA-256
  over source, destination and amount, does not match, so the request is
  rejected with `409` instead of being answered with an unrelated result.
- **Across restarts and instances.** The mechanism is a database constraint, so
  it does not depend on process state.

`replay` has one further case. If the insert conflicts but the record cannot be
read back — which retention cleanup of old keys would cause — the original
outcome cannot be reproduced, and re-running the transfer might double spend.
That is reported as `409 CONCURRENT_REQUEST` so the client retries, rather than
being served a guess. Nothing prunes `idempotency_records` today; a deployment
that adds a retention job inherits this path.

## Failure modes and recovery

| Failure | Result |
| --- | --- |
| Crash during reserve | Nothing committed; the key is free and the retry starts over |
| Crash between the two commits | The transfer stays `PENDING`; the next request with the same key re-drives settlement and completes it |
| Crash during settle | The whole settlement rolls back; the transfer stays `PENDING` and is completed as above |
| Duplicate delivery | Deduplicated by the key |
| Database unavailable | The request fails with `500`; nothing partial is left behind |

The gap this leaves is a transfer that is interrupted and never retried: it
stays `PENDING` until someone asks for that key again. A production deployment
would add a sweeper that re-drives transfers left `PENDING` past a threshold.
It would need no new logic, because settlement is already idempotent and
guarded — that is the same code path a retry takes.

## Observability

Requests that fail for an unexpected reason are logged as structured JSON with
the method, path and the full error, while the client receives a generic
`INTERNAL_ERROR`. Driver and SQLSTATE detail never crosses the API boundary; a
test asserts this. Expected outcomes carry stable machine-readable codes so
callers can branch on them without parsing prose.

## Testing strategy

Tests are behavioural and run against a real database — the same suite against
both SQLite and PostgreSQL, so the shared repository code is proven on the
engine it will actually run on.

The concurrency tests reproduce races rather than assert structure: they
release many callers simultaneously and check the resulting money. Two of them
were confirmed to fail when the mechanism under test is removed — the
opposing-transfer test deadlocks without the lock ordering, and the overspend
test trips the `balance >= 0` constraint without the row locks.

Every test that moves money also asserts the standing invariants: debits equal
credits, a `PROCESSED` transfer has exactly two entries while `PENDING` and
`FAILED` transfers have none, and each wallet's stored balance equals its
opening balance plus the entries recorded against it.

## Tradeoffs

- **Stored balance rather than derived.** Deriving every balance from the
  ledger avoids a denormalised field but makes every transfer scan the wallet's
  history. The stored balance is kept honest by updating it in the same
  transaction as the entries, by the `balance >= 0` constraint, and by the test
  that reconciles the two after every scenario.
- **Pessimistic locking rather than optimistic.** Transfers contend on hot
  wallets, where optimistic retries would mostly be wasted work. Pessimistic
  locks with a fixed ordering are also easier to reason about, which matters
  more than throughput at this scale.
- **Two transactions rather than one.** A single transaction would be simpler
  but would either lose the `PENDING` state entirely or be unable to record a
  `FAILED` outcome, since recording the failure has to survive the rollback of
  the money movement. The cost is the interrupted-`PENDING` window described
  above.
- **Two supported databases.** PostgreSQL is the real target; SQLite exists so
  the suite runs anywhere with no services, which is what the repository's CI
  provides. The dialect differences are confined to one file.

# Service Layer Design

## Problem Statement

The service layer owns all business logic. It orchestrates idempotency
checking, wallet locking, balance validation, double-entry ledger recording,
and transfer state transitions — without knowing anything about HTTP or SQL.

## Expected Behaviour

`TransferService.Execute` either:
- Returns a successful `TransferResponse` (first execution or idempotent replay).
- Returns a domain error (`ErrInsufficientFunds`, `ErrWalletNotFound`, etc.).
- Returns a wrapped infrastructure error with the originating layer prefix.

## API / Event Contract

### Input — `TransferRequest`

| Field | Type | Constraints |
|---|---|---|
| `IdempotencyKey` | string | required, unique per unique transfer intent |
| `FromWalletID` | uuid.UUID | must exist; must differ from `ToWalletID` |
| `ToWalletID` | uuid.UUID | must exist |
| `Amount` | domain.Amount | must be > 0 (minor units / cents) |

### Output — `TransferResponse`

| Field | JSON key | Description |
|---|---|---|
| `TransferID` | `"id"` | UUID v7 of the created transfer |
| `Status` | `"status"` | integer `TransferStatus` (2 = PROCESSED) |
| `Amount` | `"amount"` | amount that was transferred |

## Transfer Workflow

```
validate inputs
  ├─ amount <= 0 → ErrInvalidAmount
  └─ fromID == toID → ErrSameWallet

idempotency lookup (idempotency_records)
  └─ hit → return cached TransferResponse (no DB writes)

BEGIN transaction

lock wallets FOR UPDATE in ascending UUID byte order
  └─ not found → ErrWalletNotFound

check fromWallet.balance >= amount
  └─ fail → ErrInsufficientFunds, ROLLBACK

INSERT transfer (status=PENDING)
UPDATE wallets (debit from, credit to)
INSERT ledger_entries (DEBIT + CREDIT)
UPDATE transfer status → PROCESSED
INSERT idempotency_records (full JSON response)

COMMIT
```

## Side Effects

Every state transition is logged via `slog` with these structured fields:

```
"layer":"svc"
"idempotencyKey":"..."
"transferID":"..."
"fromWalletID":"..."
"toWalletID":"..."
"from":"PENDING"
"to":"PROCESSED"
```

Errors are logged with `"layer":"svc"` and the originating wallet/transfer ID.

## Failure Modes

| Condition | Error | Behaviour |
|---|---|---|
| Amount ≤ 0 | `ErrInvalidAmount` | Rejected before any DB call |
| Source == destination | `ErrSameWallet` | Rejected before any DB call |
| Wallet not found | `ErrWalletNotFound` | Rolled back |
| Balance < amount | `ErrInsufficientFunds` | Rolled back |
| DB/infrastructure error | wrapped error | Rolled back, error propagated |
| Commit fails | wrapped error | Rolled back automatically by defer |

## Idempotency Behaviour

1. **Check first:** `idempotency_records` is read before any write. Cache hit
   returns the stored JSON immediately — zero DB writes.
2. **Write last:** The idempotency record is inserted inside the same
   transaction as the transfer and ledger entries. If the commit fails, no
   record is stored and the next retry will re-execute normally.
3. **Race guard:** `transfers.idempotency_key` has a UNIQUE constraint. If two
   goroutines race on a brand-new key, only one INSERT wins; the other receives
   a `23505` unique violation which the service can detect and retry the
   idempotency lookup.

## Concurrency Handling

Wallets are locked with `SELECT … FOR UPDATE` inside a database transaction.
Locks are acquired in ascending UUID byte-order to prevent deadlocks when two
concurrent transfers target the same pair of wallets in opposite directions.

```
Transfer A: wallet_1 → wallet_2  locks: wallet_1 first, wallet_2 second
Transfer B: wallet_2 → wallet_1  locks: wallet_1 first, wallet_2 second
```

No deadlock can occur because both acquire in the same order.

## Retry Behaviour

All operations inside the transaction are retry-safe:
- If the process dies mid-transaction, PostgreSQL rolls it back automatically.
- The next retry hits the idempotency check. If the first attempt committed
  the idempotency record, the cached response is returned. If not, the
  transfer is re-executed.

## Consistency Expectations

- Debit and credit always equal (same amount, opposite types).
- `wallets.balance >= 0` enforced by DB constraint and application check.
- Transfer and ledger entries are always committed together.

## Testing Strategy

Unit tests in `internal/service/service_test.go` use in-memory fakes for all
four repository interfaces. No Docker or database is needed — tests run in
milliseconds. Covers all error paths and the idempotency fast-path.

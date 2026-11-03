# Database documentation

## Storage engine

The service uses SQLite via the `modernc.org/sqlite` driver. The database file is configured with `WALLET_DB_PATH`, defaulting to `wallets.sqlite3` in the project root.

## Schema overview

### wallets

```sql
CREATE TABLE IF NOT EXISTS wallets (
    wallet_id TEXT PRIMARY KEY,
    balance_minor INTEGER NOT NULL CHECK (balance_minor >= 0),
    created_at TEXT NOT NULL
);
```

Purpose:

- stores wallet identity and current balance
- each wallet has a unique `wallet_id`
- `balance_minor` is always non-negative to ensure no account can go below zero

### transfers

```sql
CREATE TABLE IF NOT EXISTS transfers (
    transfer_id TEXT PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    request_hash TEXT NOT NULL,
    from_wallet_id TEXT NOT NULL REFERENCES wallets(wallet_id),
    to_wallet_id TEXT NOT NULL REFERENCES wallets(wallet_id),
    amount_minor INTEGER NOT NULL CHECK (amount_minor > 0),
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_code TEXT,
    created_at TEXT NOT NULL,
    CHECK (from_wallet_id <> to_wallet_id),
    CHECK (
        (status = 'FAILED' AND failure_code IS NOT NULL)
        OR (status <> 'FAILED' AND failure_code IS NULL)
    )
);
```

Purpose:

- stores each transfer record
- tracks source and destination wallets
- stores the canonical request hash for idempotency verification
- records final status and failure reason

### ledger_entries

```sql
CREATE TABLE IF NOT EXISTS ledger_entries (
    entry_id INTEGER PRIMARY KEY AUTOINCREMENT,
    wallet_id TEXT NOT NULL REFERENCES wallets(wallet_id),
    transfer_id TEXT REFERENCES transfers(transfer_id),
    entry_type TEXT NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount_minor INTEGER NOT NULL CHECK (amount_minor > 0),
    created_at TEXT NOT NULL,
    CHECK (transfer_id IS NOT NULL OR entry_type = 'CREDIT'),
    UNIQUE (transfer_id, entry_type)
);
```

Purpose:

- records immutable movement history for wallets
- supports debit and credit entries for each transfer
- allows an opening balance to be represented as a `CREDIT` entry with `transfer_id IS NULL`

## Indexes and constraints

The database uses these additional protections:

- `idx_ledger_wallet_created` on `(wallet_id, created_at)`
- unique opening-entry index so only one opening credit can exist per wallet
- foreign key enforcement via `PRAGMA foreign_keys = ON`
- those checks ensure a transfer's ledger entries match the transfer amount, wallet, and type

## Trigger-based invariants

The schema enforces several critical rules with triggers.

### validate_transfer_ledger_entry

Ensures ledger entries for a transfer are only allowed when the transfer is still in `PENDING` and the ledger amount, source wallet, and destination wallet all match the transfer.

### validate_processed_transfer

When a transfer transitions from `PENDING` to `PROCESSED`, it verifies:

- exactly two ledger entries are present
- one is a debit from the source wallet
- one is a credit to the destination wallet
- both record the transfer amount

### validate_failed_transfer

When a transfer transitions from `PENDING` to `FAILED`, it ensures no ledger entries exist for that transfer.

### validate_transfer_state_transition

Only allows state changes from `PENDING` to either `PROCESSED` or `FAILED`.

### prevent_ledger_entry_update and prevent_ledger_entry_delete

Ledger entries are immutable once written.

## Data flow

1. A wallet is inserted into `wallets`.
2. If the wallet has an opening balance, a `CREDIT` entry is inserted into `ledger_entries` with `transfer_id = NULL`.
3. A transfer is inserted as `PENDING` with a request hash.
4. For a successful transfer:
   - one `DEBIT` ledger entry is inserted for the source wallet
   - one `CREDIT` ledger entry is inserted for the destination wallet
   - balances are updated in `wallets`
   - the transfer status is marked `PROCESSED`
5. For a failed transfer:
   - no ledger entries are created
   - the transfer status is marked `FAILED`

## Why this model is safe

This design provides:

- exact money accounting using integer minor units
- replay protection with `idempotency_key` and `request_hash`
- durable and atomic commit semantics via SQLite transactions
- integrity guarantees through SQL constraints and database triggers
- a complete audit trail via the `ledger_entries` table

package constants

// LockWalletsSQL takes the wallet row locks for one transfer.
//
// Two properties matter:
//
//  1. ORDER BY id: rows are locked in a globally consistent order, so two
//     transfers touching the same pair of wallets can never hold locks in
//     opposite orders and deadlock (see README, "Deadlock freedom").
//  2. FOR NO KEY UPDATE instead of FOR UPDATE: it serializes wallet
//     modifications just as well, but does not conflict with the FOR KEY
//     SHARE locks that foreign key checks take, so unrelated inserts
//     referencing a hot wallet cannot block behind (or ahead of) this lock.
const LockWalletsSQL = `
SELECT id, balance, currency, created_at, updated_at
FROM wallets
WHERE id = ANY($1::text[])
ORDER BY id
FOR NO KEY UPDATE`

// DebitWalletSQL subtracts from the balance only if the result stays >= 0.
// The guard clause is the single enforcement point for sufficient funds:
// with the row already locked, "0 rows updated" means the balance is too low.
const DebitWalletSQL = `
UPDATE wallets
SET balance = balance - $2, updated_at = now()
WHERE id = $1 AND balance >= $2`

const CreditWalletSQL = `
UPDATE wallets
SET balance = balance + $2, updated_at = now()
WHERE id = $1
  AND balance <= 9223372036854775807 - $2`

const GetWalletSQL = `
SELECT id, balance, currency, created_at, updated_at
FROM wallets
WHERE id = $1`

const CreateTransferSQL = `
INSERT INTO transfers (from_wallet_id, to_wallet_id, amount)
VALUES ($1, $2, $3)
RETURNING id::text, from_wallet_id, to_wallet_id, amount, status,
          coalesce(failure_reason, ''), created_at, updated_at`

// MarkProcessedSQL and MarkFailedSQL are guarded by status = 'PENDING', which
// makes the state machine terminal: a repeated call matches 0 rows and is
// reported as an internal error instead of silently overwriting a terminal
// state.
const MarkProcessedSQL = `
UPDATE transfers
SET status = 'PROCESSED', updated_at = now()
WHERE id = $1::uuid AND status = 'PENDING'`

const MarkFailedSQL = `
UPDATE transfers
SET status = 'FAILED', failure_reason = $2, updated_at = now()
WHERE id = $1::uuid AND status = 'PENDING'`

const GetTransferSQL = `
SELECT id::text, from_wallet_id, to_wallet_id, amount, status,
       coalesce(failure_reason, ''), created_at, updated_at
FROM transfers
WHERE id = $1::uuid`

// ClaimIdempotencySQL claims the key or reports a conflict. ON CONFLICT DO
// NOTHING makes the primary key the arbiter: exactly one concurrent request
// can claim a key, and a conflicting request waits for the winner's
// transaction to finish before learning that it lost.
const ClaimIdempotencySQL = `
INSERT INTO idempotency_records (idempotency_key, request_body)
VALUES ($1, $2::jsonb)
ON CONFLICT (idempotency_key) DO NOTHING`

// GetIdempotencyRecordSQL compares the stored request body to the canonical
// request inside PostgreSQL, so equality follows jsonb semantics on exactly one
// normalization (the canonical bytes produced by the HTTP layer).
const GetIdempotencyRecordSQL = `
SELECT request_body = $2::jsonb,
       transfer_id::text,
       response_status::int,
       response_body
FROM idempotency_records
WHERE idempotency_key = $1`

// CompleteIdempotencySQL records a claimed key's outcome. The
// response_status IS NULL guard makes "only a claimed record can be completed"
// structural: completing an already-complete key matches zero rows and trips
// the rowcount assertion in CompleteIdempotency instead of silently
// overwriting a recorded outcome.
const CompleteIdempotencySQL = `
UPDATE idempotency_records
SET transfer_id = $2::uuid,
    response_status = $3,
    response_body = $4,
    updated_at = now()
WHERE idempotency_key = $1
  AND response_status IS NULL`

// InsertLedgerEntriesSQL writes both sides of the double-entry pair in a
// single statement. The rows are immutable: there is no update or delete path,
// and the schema's unique (transfer_id, entry_type) constraint makes duplicates
// impossible even if this statement were somehow reached twice.
const InsertLedgerEntriesSQL = `
INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount)
VALUES ($1::uuid, $2, 'DEBIT', $4),
       ($1::uuid, $3, 'CREDIT', $4)`

const ListWalletLedgerSQL = `
SELECT id, transfer_id::text, wallet_id, entry_type, amount, created_at
FROM ledger_entries
WHERE wallet_id = $1
ORDER BY id DESC
LIMIT $2`

// SetLocalTransactionTimeoutsSQL sets per-transaction lock and statement
// budgets and defers both constraint triggers that need to run at COMMIT:
//   - ledger_entries_require_processed_transfer: InsertLedgerEntries runs while
//     the transfer is still PENDING; deferring lets the check see the final
//     PROCESSED state at commit.
//   - transfers_processed_requires_ledger_pair: MarkProcessed fires before the
//     ledger rows are visible to a statement-time trigger; deferring ensures
//     the pair check runs after both writes have landed.
const SetLocalTransactionTimeoutsSQL = "SET LOCAL lock_timeout = %d; SET LOCAL statement_timeout = %d; SET CONSTRAINTS ledger_entries_require_processed_transfer, transfers_processed_requires_ledger_pair DEFERRED"

const SeedWalletsSQL = `
INSERT INTO wallets (id, balance, currency)
VALUES ('wallet_1', 1000, 'USD'), ('wallet_2', 500, 'USD')
ON CONFLICT (id) DO NOTHING`

const ListSeedWalletsSQL = `SELECT id, balance, currency FROM wallets ORDER BY id`

const MigrationAdvisoryLockSQL = "SELECT pg_advisory_lock($1)"

const MigrationAdvisoryUnlockSQL = "SELECT pg_advisory_unlock($1)"

const CreateSchemaMigrationsTableSQL = `CREATE TABLE IF NOT EXISTS schema_migrations (
	version    text        PRIMARY KEY,
	applied_at timestamptz NOT NULL DEFAULT now()
)`

const SelectSchemaMigrationVersionsSQL = "SELECT version FROM schema_migrations"

const RecordSchemaMigrationVersionSQL = "INSERT INTO schema_migrations (version) VALUES ($1)"

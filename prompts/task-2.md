# AGENT ROLE & RESPONSIBILITIES

### Role
You are an autonomous Senior Go Backend Engineer and Database Specialist executing inside the Antigravity CLI environment. You implement clean repository abstractions, atomic SQLite transaction runners, deterministic entity ordering, and lease-based idempotency persistence.

### Core Responsibilities
1. **Contract & Schema Alignment**: Strictly adhere to the domain entities and table structures defined in Milestone 1:
   - Terminology: `wallets` (not `accounts`), `from_wallet_id`, `to_wallet_id`.
   - Numeric Precision: `int64` minor units (cents) only. Never use floats.
2. **SQLite Transaction Safety**:
   - Mutating transactions must execute with immediate write reservation (`BEGIN IMMEDIATE`) to prevent lock escalation deadlocks (`SQLITE_BUSY`).
   - SQLite parameter placeholders must strictly use `?`.
3. **Deterministic ID Ordering**: Sort wallet IDs lexicographically (`min(fromID, toID)` then `max(...)`) prior to applying balance mutations to ensure predictable lock acquisition order.
4. **Two-Phase Idempotency Protocol**:
   - `Reserve`: Insert or claim an `IN_PROGRESS` lease record with `locked_until` (e.g., now + 15 seconds) inside an independent transaction.
   - `Resolve`: Commit the finalized response code and body with status `RESOLVED`.
   - `Release`: Clean up or mark failed leases as `FAILED` to allow client retries upon validation failure.
5. **Compilation Verification**: Conclude by running validation commands (`go vet ./...` and `go build ./...`) to ensure clean compilation.

---

# TASK: Milestone 2 - Repository Layer (Atomic Transactions, Wallets, Transfers & Idempotency)

### 1. WHAT (Files to Create)
Implement the storage interfaces, transaction helper, and concrete SQLite repository implementations covering wallet balances, transfers, immutable double-entry ledger postings, and durable idempotency leasing.

#### Target Files:
1. `internal/repository/interfaces.go`
2. `internal/repository/tx_manager.go`
3. `internal/repository/wallet_repository.go`
4. `internal/repository/transfer_repository.go`
5. `internal/repository/idempotency_repository.go`

---

### 2. WHY
- **Double-Entry Guarantees**: A transfer cannot simply update numbers; debit, credit, and transfer state rows must commit atomically inside the same transaction.
- **SQLite Concurrency & Immediate Write Locks**: Starting transactions with standard `BEGIN` begins in shared read mode and escalates on the first write. If two transactions escalate concurrently, SQLite triggers a busy deadlock. Executing `BEGIN IMMEDIATE` acquires the write lock at transaction start.
- **Observable In-Flight Idempotency**: Storing `IN_PROGRESS` with a time-limited lease (`locked_until`) in an independent transaction boundary ensures concurrent duplicates observe in-flight transfers and receive `409 Conflict`, while crashed operations expire safely.
- **Deterministic ID Ordering**: Sorting wallet IDs before issuing `UPDATE` statements establishes clean discipline and avoids lock contention patterns.

---

### 3. WHEN
Execute immediately after **Milestone 1** (domain models, errors, SQLite connection setup, and migrations are compiled and verified).

---

### 4. HOW (Exact File Implementations)

#### Step 4.1: Create `internal/repository/interfaces.go`
Define clean repository and transaction manager contracts:

```go
package repository

import (
	"context"
	"database/sql"
	"time"

	"[github.com/Robustrade/wallet-transfer-assignment/internal/domain](https://github.com/Robustrade/wallet-transfer-assignment/internal/domain)"
)

// TxManager executes a unit of work inside an atomic database transaction
type TxManager interface {
	RunInTx(ctx context.Context, fn func(tx *sql.Tx) error) error
}

// WalletRepository manages wallet persistence and atomic balance updates
type WalletRepository interface {
	Create(ctx context.Context, tx *sql.Tx, wallet *domain.Wallet) error
	GetByID(ctx context.Context, tx *sql.Tx, id string) (*domain.Wallet, error)
	UpdateBalances(ctx context.Context, tx *sql.Tx, fromWalletID, toWalletID string, amount int64) error
}

// TransferRepository handles transfers and double-entry ledger records
type TransferRepository interface {
	CreateTransferAndEntries(
		ctx context.Context,
		tx *sql.Tx,
		transfer *domain.Transfer,
		debitEntry *domain.LedgerEntry,
		creditEntry *domain.LedgerEntry,
	) error
}

// IdempotencyRepository manages lease-based duplicate detection and response caching
type IdempotencyRepository interface {
	Get(ctx context.Context, tx *sql.Tx, key string) (*domain.IdempotencyRecord, error)
	Reserve(ctx context.Context, key, requestHash string, ttl time.Duration) (*domain.IdempotencyRecord, error)
	Resolve(ctx context.Context, tx *sql.Tx, key string, statusCode int, responseBody string) error
	Release(ctx context.Context, key string) error
}

```

---

#### Step 4.2: Create `internal/repository/tx_manager.go`

Implement `RunInTx` enforcing `BEGIN IMMEDIATE` for SQLite write safety:

```go
package repository

import (
	"context"
	"database/sql"
	"fmt"
)

type SQLiteTxManager struct {
	db *sql.DB
}

func NewSQLiteTxManager(db *sql.DB) *SQLiteTxManager {
	return &SQLiteTxManager{db: db}
}

// RunInTx executes fn inside a transaction started with BEGIN IMMEDIATE
func (m *SQLiteTxManager) RunInTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	// Standard database/sql BeginTx with default options does not issue BEGIN IMMEDIATE in modernc.org/sqlite.
	// Executing BEGIN IMMEDIATE directly reserves the write lock upfront.
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	if _, err := tx.ExecContext(ctx, "ROLLBACK; BEGIN IMMEDIATE;"); err != nil {
		// Fallback: if driver supports immediate or dialect rejects rollback prior to exec
		_ = tx.Rollback()
		tx, err = m.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return fmt.Errorf("failed to begin serializable transaction: %w", err)
		}
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil && rbErr != sql.ErrTxDone {
			return fmt.Errorf("tx error: %v, rollback error: %w", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

```

---

#### Step 4.3: Create `internal/repository/wallet_repository.go`

Implement wallet operations with deterministic ID sorting for balance mutations:

```go
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"[github.com/Robustrade/wallet-transfer-assignment/internal/domain](https://github.com/Robustrade/wallet-transfer-assignment/internal/domain)"
)

type SQLiteWalletRepository struct {
	db *sql.DB
}

func NewSQLiteWalletRepository(db *sql.DB) *SQLiteWalletRepository {
	return &SQLiteWalletRepository{db: db}
}

type queryExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (r *SQLiteWalletRepository) getExecutor(tx *sql.Tx) queryExecutor {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *SQLiteWalletRepository) Create(ctx context.Context, tx *sql.Tx, wallet *domain.Wallet) error {
	query := `
		INSERT INTO wallets (id, user_id, currency, balance, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?);
	`
	exec := r.getExecutor(tx)
	now := time.Now().UTC()
	if wallet.CreatedAt.IsZero() {
		wallet.CreatedAt = now
	}
	if wallet.UpdatedAt.IsZero() {
		wallet.UpdatedAt = now
	}

	_, err := exec.ExecContext(ctx, query,
		wallet.ID,
		wallet.UserID,
		wallet.Currency,
		wallet.Balance,
		wallet.CreatedAt,
		wallet.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert wallet: %w", err)
	}
	return nil
}

func (r *SQLiteWalletRepository) GetByID(ctx context.Context, tx *sql.Tx, id string) (*domain.Wallet, error) {
	query := `
		SELECT id, user_id, currency, balance, created_at, updated_at
		FROM wallets
		WHERE id = ?;
	`
	exec := r.getExecutor(tx)
	row := exec.QueryRowContext(ctx, query, id)

	var w domain.Wallet
	err := row.Scan(&w.ID, &w.UserID, &w.Currency, &w.Balance, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, fmt.Errorf("failed to query wallet by id %s: %w", id, err)
	}
	return &w, nil
}

// UpdateBalances applies balance mutations in deterministic ID order
func (r *SQLiteWalletRepository) UpdateBalances(ctx context.Context, tx *sql.Tx, fromWalletID, toWalletID string, amount int64) error {
	if fromWalletID == toWalletID {
		return domain.ErrSelfTransfer
	}
	if amount <= 0 {
		return domain.ErrInvalidAmount
	}

	exec := r.getExecutor(tx)
	now := time.Now().UTC()

	// Deterministic sorting to guarantee ordered execution
	firstID, secondID := fromWalletID, toWalletID
	firstDelta, secondDelta := -amount, amount

	if firstID > secondID {
		firstID, secondID = toWalletID, fromWalletID
		firstDelta, secondDelta = amount, -amount
	}

	updateQuery := `
		UPDATE wallets
		SET balance = balance + ?, updated_at = ?
		WHERE id = ?;
	`

	// First update
	res1, err := exec.ExecContext(ctx, updateQuery, firstDelta, now, firstID)
	if err != nil {
		return fmt.Errorf("failed to update wallet %s: %w", firstID, err)
	}
	rows1, err := res1.RowsAffected()
	if err != nil || rows1 == 0 {
		return fmt.Errorf("wallet %s not found for update: %w", firstID, domain.ErrWalletNotFound)
	}

	// Second update
	res2, err := exec.ExecContext(ctx, updateQuery, secondDelta, now, secondID)
	if err != nil {
		return fmt.Errorf("failed to update wallet %s: %w", secondID, err)
	}
	rows2, err := res2.RowsAffected()
	if err != nil || rows2 == 0 {
		return fmt.Errorf("wallet %s not found for update: %w", secondID, domain.ErrWalletNotFound)
	}

	return nil
}

```

---

#### Step 4.4: Create `internal/repository/transfer_repository.go`

Insert transfer intents and paired ledger entries atomically:

```go
package repository

import (
	"context"
	"database/sql"
	"fmt"

	"[github.com/Robustrade/wallet-transfer-assignment/internal/domain](https://github.com/Robustrade/wallet-transfer-assignment/internal/domain)"
)

type SQLiteTransferRepository struct {
	db *sql.DB
}

func NewSQLiteTransferRepository(db *sql.DB) *SQLiteTransferRepository {
	return &SQLiteTransferRepository{db: db}
}

func (r *SQLiteTransferRepository) CreateTransferAndEntries(
	ctx context.Context,
	tx *sql.Tx,
	transfer *domain.Transfer,
	debitEntry *domain.LedgerEntry,
	creditEntry *domain.LedgerEntry,
) error {
	var exec queryExecutor = tx
	if exec == nil {
		exec = r.db
	}

	// 1. Insert Transfer Intent
	transferQuery := `
		INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, currency, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?);
	`
	_, err := exec.ExecContext(ctx, transferQuery,
		transfer.ID,
		transfer.IdempotencyKey,
		transfer.FromWalletID,
		transfer.ToWalletID,
		transfer.Amount,
		transfer.Currency,
		string(transfer.Status),
		transfer.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert transfer record: %w", err)
	}

	// 2. Insert Debit and Credit Ledger Entries
	ledgerQuery := `
		INSERT INTO ledger_entries (id, transfer_id, wallet_id, amount, direction, created_at)
		VALUES (?, ?, ?, ?, ?, ?);
	`

	// Debit entry
	_, err = exec.ExecContext(ctx, ledgerQuery,
		debitEntry.ID,
		debitEntry.TransferID,
		debitEntry.WalletID,
		debitEntry.Amount,
		string(debitEntry.Direction),
		debitEntry.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert debit ledger entry: %w", err)
	}

	// Credit entry
	_, err = exec.ExecContext(ctx, ledgerQuery,
		creditEntry.ID,
		creditEntry.TransferID,
		creditEntry.WalletID,
		creditEntry.Amount,
		string(creditEntry.Direction),
		creditEntry.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert credit ledger entry: %w", err)
	}

	return nil
}

```

---

#### Step 4.5: Create `internal/repository/idempotency_repository.go`

Implement lease reservation, resolution, and release:

```go
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"[github.com/Robustrade/wallet-transfer-assignment/internal/domain](https://github.com/Robustrade/wallet-transfer-assignment/internal/domain)"
)

type SQLiteIdempotencyRepository struct {
	db *sql.DB
}

func NewSQLiteIdempotencyRepository(db *sql.DB) *SQLiteIdempotencyRepository {
	return &SQLiteIdempotencyRepository{db: db}
}

func (r *SQLiteIdempotencyRepository) Get(ctx context.Context, tx *sql.Tx, key string) (*domain.IdempotencyRecord, error) {
	query := `
		SELECT key, request_hash, status, response_code, response_body, created_at, locked_until
		FROM idempotency_records
		WHERE key = ?;
	`
	var exec queryExecutor = tx
	if exec == nil {
		exec = r.db
	}

	row := exec.QueryRowContext(ctx, query, key)
	var rec domain.IdempotencyRecord
	var respCode sql.NullInt64
	var respBody sql.NullString
	var status string

	err := row.Scan(
		&rec.Key,
		&rec.RequestHash,
		&status,
		&respCode,
		&respBody,
		&rec.CreatedAt,
		&rec.LockedUntil,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query idempotency record: %w", err)
	}

	rec.Status = domain.IdempotencyStatus(status)
	if respCode.Valid {
		rec.ResponseCode = int(respCode.Int64)
	}
	if respBody.Valid {
		rec.ResponseBody = respBody.String
	}

	return &rec, nil
}

// Reserve attempts to obtain an IN_PROGRESS lease inside an immediate write transaction
func (r *SQLiteIdempotencyRepository) Reserve(ctx context.Context, key, requestHash string, ttl time.Duration) (*domain.IdempotencyRecord, error) {
	now := time.Now().UTC()
	lockedUntil := now.Add(ttl)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin reservation tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	existing, err := r.Get(ctx, tx, key)
	if err != nil {
		return nil, err
	}

	if existing != nil {
		// Existing resolved request: return for cached replay or mismatch check
		if existing.Status == domain.IdempotencyStatusResolved {
			return existing, nil
		}

		// In-progress request: check whether lock lease has expired
		if existing.Status == domain.IdempotencyStatusInProgress && now.Before(existing.LockedUntil) {
			return existing, domain.ErrConcurrentRequest
		}

		// Reclaim expired lease or failed attempt
		updateQuery := `
			UPDATE idempotency_records
			SET request_hash = ?, status = ?, locked_until = ?
			WHERE key = ?;
		`
		_, err = tx.ExecContext(ctx, updateQuery, requestHash, string(domain.IdempotencyStatusInProgress), lockedUntil, key)
		if err != nil {
			return nil, fmt.Errorf("failed to reclaim idempotency lease: %w", err)
		}
	} else {
		// Fresh key insertion
		insertQuery := `
			INSERT INTO idempotency_records (key, request_hash, status, created_at, locked_until)
			VALUES (?, ?, ?, ?, ?);
		`
		_, err = tx.ExecContext(ctx, insertQuery, key, requestHash, string(domain.IdempotencyStatusInProgress), now, lockedUntil)
		if err != nil {
			return nil, fmt.Errorf("failed to insert idempotency lease: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit reservation tx: %w", err)
	}

	return &domain.IdempotencyRecord{
		Key:         key,
		RequestHash: requestHash,
		Status:      domain.IdempotencyStatusInProgress,
		CreatedAt:   now,
		LockedUntil: lockedUntil,
	}, nil
}

// Resolve commits cached response payload and marks status as RESOLVED
func (r *SQLiteIdempotencyRepository) Resolve(ctx context.Context, tx *sql.Tx, key string, statusCode int, responseBody string) error {
	query := `
		UPDATE idempotency_records
		SET status = ?, response_code = ?, response_body = ?
		WHERE key = ?;
	`
	var exec queryExecutor = tx
	if exec == nil {
		exec = r.db
	}

	_, err := exec.ExecContext(ctx, query, string(domain.IdempotencyStatusResolved), statusCode, responseBody, key)
	if err != nil {
		return fmt.Errorf("failed to resolve idempotency record %s: %w", key, err)
	}
	return nil
}

// Release deletes or marks record as FAILED to allow clean retries on validation failure
func (r *SQLiteIdempotencyRepository) Release(ctx context.Context, key string) error {
	query := `DELETE FROM idempotency_records WHERE key = ? AND status = ?;`
	_, err := r.db.ExecContext(ctx, query, key, string(domain.IdempotencyStatusInProgress))
	if err != nil {
		return fmt.Errorf("failed to release idempotency record %s: %w", key, err)
	}
	return nil
}

```

---

### 5. CLI EXECUTION & VERIFICATION PROTOCOL

Run these validation commands to verify the setup:

1. Re-evaluate dependencies and tidy:

```bash
go mod tidy

```

2. Run compiler type check and syntax vetting:

```bash
go vet ./...

```

3. Confirm all packages compile without errors:

```bash
go build ./...

```

**Success Criteria**:

* `go vet ./...` completes with 0 warnings or errors.
* `go build ./...` exits with code 0.
* All repository interfaces correctly import and reference `internal/domain` models and sentinel errors.
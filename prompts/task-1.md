# AGENT ROLE & RESPONSIBILITIES

### Role
You are an autonomous Senior Go Backend Engineer and Database Specialist executing inside the Antigravity CLI environment. You implement clean, robust domain logic, pure-Go SQLite initialization, and strict schema DDL.

### Core Responsibilities
1. **Contract & Schema Alignment**: Adhere strictly to the `ASSIGNMENT.md` specification. Use `wallets` (not `accounts`), `from_wallet_id` / `to_wallet_id`, and payload-based `idempotencyKey`.
2. **Numeric Precision**: Statically enforce `int64` minor units (cents) across all models. Floating-point numbers (`float32`, `float64`) are strictly forbidden.
3. **Database Integrity**: Guarantee that schema DDL enforces hard database constraints (`CHECK (balance >= 0)`, `CHECK (amount > 0)`, `ON DELETE RESTRICT`).
4. **Pure-Go Driver & SQLite PRAGMAs**: Initialize SQLite using `modernc.org/sqlite` with `WAL` mode, busy timeouts, and serialized connection pooling.
5. **Compilation Verification**: Conclude by running validation commands (`go vet ./...` and `go build ./...`) to ensure flawless compilation before finishing.

---

# TASK: Milestone 1 - Domain Entities, SQLite Connection & Schema DDL

### 1. WHAT (Files to Create)
Implement the core financial domain models, domain-specific sentinel errors, pure-Go SQLite connection manager with migration runner, and the initial schema migration.

#### Target Files:
1. `internal/domain/models.go`
2. `internal/domain/errors.go`
3. `internal/repository/sqlite.go`
4. `migrations/0001_init.sql`

---

### 2. WHY
- **Domain Modeling**: Financial entities must be defined with unambiguous types (`int64` for minor currency units) before any repository queries or HTTP handlers are created.
- **Contract Adherence**: Sticking directly to `wallets` and `fromWalletId`/`toWalletId` eliminates translation layers and prevents incompatibilities with the assignment grading suite.
- **SQLite Concurrency Preparation**:
  - `PRAGMA journal_mode = WAL;` allows non-blocking reads while a write transaction executes.
  - `PRAGMA busy_timeout = 5000;` prevents immediate `SQLITE_BUSY` contention by retrying lock acquisition for up to 5 seconds.
  - `PRAGMA foreign_keys = ON;` enforces referential integrity across wallets, transfers, and ledger entries.
  - `db.SetMaxOpenConns(1)` serializes database writes to prevent connection-level lock collisions.
- **Database-Level Defense**: `CHECK (balance >= 0)` ensures that an overdraft cannot physically write to disk regardless of application logic bugs.

---

### 3. WHEN
Execute immediately after **Milestone 0** project bootstrap, before implementing the repository layer or business service logic.

---

### 4. HOW (Exact File Implementations)

#### Step 4.1: Create `migrations/0001_init.sql`
Create raw SQL definitions with primary keys, foreign key constraints, indexes, and check validations:

```sql
-- Wallets table
CREATE TABLE IF NOT EXISTS wallets (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    currency TEXT NOT NULL,
    balance INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    CONSTRAINT chk_wallet_balance CHECK (balance >= 0)
);

CREATE INDEX IF NOT EXISTS idx_wallets_user_id ON wallets(user_id);

-- Transfers table (represents high-level transfer intent)
CREATE TABLE IF NOT EXISTS transfers (
    id TEXT PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    from_wallet_id TEXT NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    to_wallet_id TEXT NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    amount INTEGER NOT NULL CHECK (amount > 0),
    currency TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'COMPLETED', 'FAILED')),
    created_at DATETIME NOT NULL,
    CONSTRAINT chk_distinct_wallets CHECK (from_wallet_id <> to_wallet_id)
);

CREATE INDEX IF NOT EXISTS idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX IF NOT EXISTS idx_transfers_to_wallet ON transfers(to_wallet_id);

-- Ledger entries table (immutable double-entry audit trail)
CREATE TABLE IF NOT EXISTS ledger_entries (
    id TEXT PRIMARY KEY,
    transfer_id TEXT NOT NULL REFERENCES transfers(id) ON DELETE RESTRICT,
    wallet_id TEXT NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    amount INTEGER NOT NULL CHECK (amount > 0),
    direction TEXT NOT NULL CHECK (direction IN ('DEBIT', 'CREDIT')),
    created_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ledger_entries_wallet_id ON ledger_entries(wallet_id);
CREATE INDEX IF NOT EXISTS idx_ledger_entries_transfer_id ON ledger_entries(transfer_id);

-- Idempotency store (lease-based concurrency and response caching)
CREATE TABLE IF NOT EXISTS idempotency_records (
    key TEXT PRIMARY KEY,
    request_hash TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('IN_PROGRESS', 'RESOLVED', 'FAILED')),
    response_code INTEGER,
    response_body TEXT,
    created_at DATETIME NOT NULL,
    locked_until DATETIME NOT NULL
);

```

---

#### Step 4.2: Create `internal/domain/models.go`

Define matching Go structs using minor currency units (`int64`):

```go
package domain

import (
	"time"
)

type Direction string

const (
	DirectionDebit  Direction = "DEBIT"
	DirectionCredit Direction = "CREDIT"
)

type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "PENDING"
	TransferStatusCompleted TransferStatus = "COMPLETED"
	TransferStatusFailed    TransferStatus = "FAILED"
)

type IdempotencyStatus string

const (
	IdempotencyStatusInProgress IdempotencyStatus = "IN_PROGRESS"
	IdempotencyStatusResolved   IdempotencyStatus = "RESOLVED"
	IdempotencyStatusFailed     IdempotencyStatus = "FAILED"
)

// Wallet represents a customer wallet balance container
type Wallet struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	Currency  string    `json:"currency"`
	Balance   int64     `json:"balance"` // Minor units (e.g., cents). Never use floats.
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Transfer records a financial movement between two wallets
type Transfer struct {
	ID             string         `json:"id"`
	IdempotencyKey string         `json:"idempotencyKey"`
	FromWalletID   string         `json:"fromWalletId"`
	ToWalletID     string         `json:"toWalletId"`
	Amount         int64          `json:"amount"` // Minor units (> 0)
	Currency       string         `json:"currency"`
	Status         TransferStatus `json:"status"`
	CreatedAt      time.Time      `json:"createdAt"`
}

// LedgerEntry is an immutable credit or debit record tied to a transfer
type LedgerEntry struct {
	ID         string    `json:"id"`
	TransferID string    `json:"transferId"`
	WalletID   string    `json:"walletId"`
	Amount     int64     `json:"amount"` // Minor units (> 0)
	Direction  Direction `json:"direction"`
	CreatedAt  time.Time `json:"createdAt"`
}

// IdempotencyRecord maintains request lease locks and cached responses
type IdempotencyRecord struct {
	Key          string            `json:"key"`
	RequestHash  string            `json:"requestHash"`
	Status       IdempotencyStatus `json:"status"`
	ResponseCode int               `json:"responseCode,omitempty"`
	ResponseBody string            `json:"responseBody,omitempty"`
	CreatedAt    time.Time         `json:"createdAt"`
	LockedUntil  time.Time         `json:"lockedUntil"`
}

```

---

#### Step 4.3: Create `internal/domain/errors.go`

Define domain-level sentinel errors:

```go
package domain

import "errors"

var (
	ErrInsufficientFunds       = errors.New("insufficient funds for transfer")
	ErrWalletNotFound          = errors.New("wallet not found")
	ErrCurrencyMismatch        = errors.New("currency mismatch between wallets or transfer request")
	ErrSelfTransfer            = errors.New("cannot transfer funds to the same wallet")
	ErrInvalidAmount           = errors.New("amount must be greater than zero")
	ErrConcurrentRequest       = errors.New("request currently in progress")
	ErrIdempotencyKeyConflict  = errors.New("idempotency key reused with differing payload parameters")
	ErrIdempotencyKeyRequired  = errors.New("idempotencyKey is required")
)

```

---

#### Step 4.4: Create `internal/repository/sqlite.go`

Implement connection setup using `embed`, applying PRAGMAs, running migrations, and configuring single-writer concurrency:

```go
package repository

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed ../../migrations/0001_init.sql
var initMigrationSQL string

// NewSQLiteDB initializes a SQLite database connection pool optimized for concurrent execution
func NewSQLiteDB(dsn string) (*sql.DB, error) {
	if dsn == "" {
		dsn = "wallet.db"
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite connection: %w", err)
	}

	// Serializes writes through one connection to eliminate database file lock contention
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Apply critical performance & integrity PRAGMAs
	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA foreign_keys = ON;",
		"PRAGMA synchronous = NORMAL;",
	}

	for _, pragma := range pragmas {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to execute %q: %w", pragma, err)
		}
	}

	// Run initial schema migration
	if _, err := db.ExecContext(ctx, initMigrationSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run initial migration: %w", err)
	}

	return db, nil
}

```

---

### 5. CLI EXECUTION & VERIFICATION PROTOCOL

Run these validation commands to verify the setup:

1. Clean up unused module requirements and verify imports:

```bash
go mod tidy

```

2. Vet codebase for any syntax or type inconsistencies:

```bash
go vet ./...

```

3. Confirm the entire project compiles cleanly:

```bash
go build ./...

```

**Success Criteria**:

* `go mod tidy` resolves `modernc.org/sqlite` cleanly.
* `migrations/0001_init.sql` compiles and embeds without path errors.
* `go build ./...` exits with code 0 and no output errors.
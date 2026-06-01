# Wallet Transfer Service — Project Context & Progress

This document aggregates the implementation plan, the current directory structure, architecture notes, and the current task checklist into a single source of truth for the ongoing structural refactoring.

---

## 1. Domain-Driven Clean Architecture

The project has been refactored from a flat structure into three distinct bounded domains under `internal/domain/`. Cross-cutting infrastructure concern abstracts live under `internal/platform/`.

### Dependency Rules
- **Inward-pointing dependencies**: Domain packages do not import any databases or drivers (e.g. `pgx`, `gorm`). They only define storage interfaces (`Repository`).
- **Isolation of Concerns**: Handlers deal with HTTP transport, services orchestrate domain events and business rules, and platform repositories implement storage-specific adapters.
- **Transaction Boundary Ownership**: `ledger.Service` owns its database transaction boundary using a platform-level `tx.Starter` interface. No raw database connection or transaction pointer escapes into other domains.

```
HTTP Request
    │
    ▼
[domain/X/handler.go]       — parses HTTP, calls Service
    │
    ▼
[domain/X/service.go]       — business logic, calls Repository interfaces
    │
    ▼
[domain/X/repository.go]    — interface only (no imports of drivers)
    │
    ▼
[platform/postgres/X_repo.go]  — concrete implementation, imports pgx
```

---

## 2. Directory Tree & Current Workspace Layout

```
wallet-transfer-assignment/
├── server/
│   ├── main.go                        # Wires everything (currently using old imports)
│   └── config/
│       └── config.go                  # Configuration loader
│
├── internal/
│   ├── domain/                        # ← Bounded domain packages
│   │   ├── wallet/                    # Domain 1: Wallet metadata
│   │   │   ├── id.go                  # UUID generation for wallet entities
│   │   │   ├── model.go               # Wallet entity, status, and validation
│   │   │   ├── repository.go          # Repository interface definition
│   │   │   ├── service.go             # Wallet service (creation and retrieval)
│   │   │   └── handler.go             # HTTP handlers: GET /wallet/:id, POST /wallets
│   │   │
│   │   ├── transfer/                  # Domain 2: Wallet transfers
│   │   │   ├── id.go                  # UUID generation for transfers
│   │   │   ├── model.go               # Transfer entity, status transitions (PENDING -> PROCESSED/FAILED)
│   │   │   ├── repository.go          # Repository interface definition
│   │   │   ├── service.go             # Transfer execution and idempotency service
│   │   │   └── handler.go             # HTTP handler: POST /transfers
│   │   │
│   │   └── ledger/                    # Domain 3: Double-entry ledger & balances
│   │       ├── model.go               # Entry & Balance entities
│   │       ├── repository.go          # LedgerRepository & BalanceRepository interfaces
│   │       ├── service.go             # Transactional balance update & double-entry write
│   │       └── handler.go             # HTTP handler: GET /wallet/balance/:walletId
│   │
│   └── platform/                      # ← Cross-cutting infrastructure
│       └── tx/
│           └── tx.go                  # Database-agnostic transactional interfaces (Starter, Tx)
│
└── task.md                            # Checklist tracking implementation progress
```

---

## 3. Core Domain Design & Signatures

### Domain 1: Wallet Metadata
- **Entity**: [model.go](file:///c:/Users/91961/Documents/Yash_Work/Kulu/wallet-transfer-assignment/internal/domain/wallet/model.go)
  ```go
  type Wallet struct {
      ID        string    // Prefix: wallet_
      OwnerID   string
      Currency  string    // Immutable
      Status    Status    // ACTIVE or INACTIVE
      CreatedAt time.Time
      UpdatedAt time.Time
  }
  ```
- **Repository Interface**: [repository.go](file:///c:/Users/91961/Documents/Yash_Work/Kulu/wallet-transfer-assignment/internal/domain/wallet/repository.go)
  ```go
  type Repository interface {
      GetByID(ctx context.Context, id string) (*Wallet, error)
      ValidateWallets(ctx context.Context, ids []string) error
      Create(ctx context.Context, w *Wallet) error
      List(ctx context.Context, ownerID string) ([]*Wallet, error)
  }
  ```

### Domain 2: Transfers
- **Entity & State Machine**: [model.go](file:///c:/Users/91961/Documents/Yash_Work/Kulu/wallet-transfer-assignment/internal/domain/transfer/model.go)
  - States: `PENDING`, `PROCESSED`, `FAILED`
  - Allowed Transitions: `PENDING` → `PROCESSED`, `PENDING` → `FAILED`
  - Unique `TxnID` behaves as the idempotency key.
- **Repository Interface**: [repository.go](file:///c:/Users/91961/Documents/Yash_Work/Kulu/wallet-transfer-assignment/internal/domain/transfer/repository.go)
  ```go
  type Repository interface {
      Create(ctx context.Context, t *Transfer) error
      GetByID(ctx context.Context, id string) (*Transfer, error)
      GetByTxnID(ctx context.Context, txnID string) (*Transfer, error)
      UpdateStatus(ctx context.Context, id string, status Status, failureReason string) error
  }
  ```
- **Transfer Orchestration Workflow**: [service.go](file:///c:/Users/91961/Documents/Yash_Work/Kulu/wallet-transfer-assignment/internal/domain/transfer/service.go#L57-L115)
  1. **Idempotency Check**: Query by caller-supplied `TxnID` (Idempotency Key). If match found, return the previous state.
  2. **Persist PENDING**: Immediately write a `PENDING` transfer record to ensure audibility even if subsequent steps fail.
  3. **Validate Entity & Wallets**: Verify transfer parameters and query source/destination wallets (must be active).
  4. **CommitToLedger**: Call ledger service to handle transactional double-entry writes and balance updates.
  5. **Mark PROCESSED**: Update state to success terminal state.

### Domain 3: Ledger & Balances
- **Entities**: [model.go](file:///c:/Users/91961/Documents/Yash_Work/Kulu/wallet-transfer-assignment/internal/domain/ledger/model.go)
  - `Entry`: Ledger entries mapped in pairs (DEBIT and CREDIT) for each transfer.
  - `Balance`: Current wallet balance.
- **Repository Interfaces**: [repository.go](file:///c:/Users/91961/Documents/Yash_Work/Kulu/wallet-transfer-assignment/internal/domain/ledger/repository.go)
  ```go
  type LedgerRepository interface {
      InsertDoubleEntry(ctx context.Context, debit, credit *Entry) error
  }

  type BalanceRepository interface {
      GetBalance(ctx context.Context, walletID string) (*Balance, error)
      DebitAndCredit(ctx context.Context, fromID, toID string, amount int64) error
  }
  ```
- **Transaction Orchestration**: [service.go](file:///c:/Users/91961/Documents/Yash_Work/Kulu/wallet-transfer-assignment/internal/domain/ledger/service.go#L50-L91)
  1. Starts database transaction (`db.BeginTx(ctx)`).
  2. Performs row locking and executes balance debit/credit updates via `balanceRepo.DebitAndCredit`.
  3. Inserts double entries (debit + credit) via `ledgerRepo.InsertDoubleEntry`.
  4. Commits the transaction (or rolls back on error).

---

## 4. Current Refactoring Progress Checklist

The checklist below represents the current implementation state updated from `task.md`.

### Completed Tasks
- [x] **Cleanup**: Remove old empty internal packages (`internal/db`, `internal/handler`, `internal/router`, `internal/service`).
- [x] **Domain: wallet**: Implementation of model, repository interface, service business logic, and HTTP handler.
- [x] **Domain: transfer**: Implementation of model state machine, repository interface, orchestration service with idempotency checks, and HTTP handler.
- [x] **Domain: ledger**: Implementation of double-entry models, repository interfaces, transactional balance updates, and HTTP balance handler.

### Pending Tasks
- [ ] **Platform: postgres**:
  - [ ] Connection setup (`connection.go`)
  - [ ] SQL migrations (`001_create_wallets.sql`, `002_create_transfers.sql`, `003_create_ledger_entries.sql`)
  - [ ] PostgreSQL adapters implementing repository interfaces:
    - [ ] `wallet_repo.go` (implementing `wallet.Repository`)
    - [ ] `transfer_repo.go` (implementing `transfer.Repository`)
    - [ ] `ledger_repo.go` (implementing `ledger.LedgerRepository` and `ledger.BalanceRepository`)
- [ ] **Router & Wiring**:
  - [ ] Wire routing mappings in `internal/router/router.go`
  - [ ] Update `server/main.go` initialization with new platform repositories and services
- [ ] **Tests**:
  - [ ] DB test helper (`tests/testhelper/db.go`)
  - [ ] Behavioral integration tests for transfers (`tests/transfer_test.go`)
  - [ ] Concurrency safety tests (`tests/concurrency_test.go`)

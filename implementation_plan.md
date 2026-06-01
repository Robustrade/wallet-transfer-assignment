# Domain-Driven Code Structure — Wallet Transfer Service

## Overview

The current `internal/` package is flat and technology-coupled (`db`, `handler`, `router`, `service`). This plan reorganises it into **three bounded domains**, each self-contained, with a **storage-agnostic repository layer** that makes swapping or stacking data stores trivial.

---

## User Review Required

> [!IMPORTANT]
> The `WalletLedger` and `WalletBalance` domains share a single database transaction boundary during a transfer. The plan places them together in one domain package (`ledger`) with their own handler and service. Please confirm this grouping is what you intended.

> [!WARNING]
> This is a **structural refactor**. All existing empty packages (`internal/db`, `internal/handler`, `internal/router`, `internal/service`) will be deleted and replaced. Since they are currently empty, there is no logic to migrate — only the wiring in `server/main.go` changes.

---

## Open Questions

> [!IMPORTANT]
> **Storage backend**: The plan uses PostgreSQL (via `pgx`) as the primary store and shows how to add a second store (e.g., Redis for read-cache). Do you want Redis included from the start, or only as a future extension point?


---

## Proposed Directory Tree

```
wallet-transfer-assignment/
├── server/
│   ├── main.go                        # wires everything, no business logic
│   └── config/
│       └── config.go
│
├── internal/
│   │
│   ├── domain/                        # ← all three bounded domains live here
│   │   │
│   │   ├── wallet/                    # Domain 1 — Wallet (metadata)
│   │   │   ├── model.go               # Wallet entity, state, validation rules
│   │   │   ├── repository.go          # WalletRepository interface
│   │   │   ├── service.go             # WalletService (business logic)
│   │   │   └── handler.go            # HTTP handler (GET /wallet/:id, POST /wallets)
│   │   │
│   │   ├── transfer/                  # Domain 2 — Transfer
│   │   │   ├── model.go               # Transfer entity, state machine (PENDING→PROCESSED/FAILED)
│   │   │   ├── repository.go          # TransferRepository interface
│   │   │   ├── service.go             # TransferService — orchestrates the full transfer workflow
│   │   │   └── handler.go            # HTTP handler (POST /transfers)
│   │   │
│   │   └── ledger/                    # Domain 3 — WalletLedger + WalletBalance (tightly coupled)
│   │       ├── model.go               # LedgerEntry + WalletBalance entities
│   │       ├── repository.go          # LedgerRepository + BalanceRepository interfaces (one file, one tx)
│   │       ├── service.go             # LedgerService — double-entry write + balance update atomically
│   │       └── handler.go            # HTTP handler (GET /wallet/balance/:id)
│   │
│   ├── platform/                      # ← cross-cutting infrastructure (no domain knowledge)
│   │   │
│   │   └── postgres/                  # Concrete storage adapter — PostgreSQL
│   │       ├── connection.go          # pgx pool setup, migrations runner
│   │       ├── migrations/            # SQL migration files (*.sql)
│   │       │   ├── 001_create_wallets.sql
│   │       │   ├── 002_create_transfers.sql
│   │       │   └── 003_create_ledger_entries.sql
│   │       ├── wallet_repo.go         # implements domain/wallet.WalletRepository
│   │       ├── transfer_repo.go       # implements domain/transfer.TransferRepository
│   │       └── ledger_repo.go         # implements domain/ledger.LedgerRepository + BalanceRepository
│   │
│   └── router/
│       └── router.go                  # mounts all domain handlers, no logic
│
└── tests/
    ├── transfer_test.go               # behavioural: transfer execution, idempotency, ledger
    ├── concurrency_test.go            # concurrent transfer safety
    └── testhelper/
        └── db.go                      # test DB setup/teardown
```

---

## Key Design Decisions

### 1. Repository Interface Pattern (storage-agnostic core)

Each domain defines its own interface in `repository.go`. **No domain package imports any DB driver.**

```go
// internal/domain/wallet/repository.go
package wallet

import "context"

type Repository interface {
    GetByID(ctx context.Context, id string) (*Wallet, error)
    Create(ctx context.Context, w *Wallet) error
    // ...
}
```

```go
// internal/domain/transfer/repository.go
package transfer

import "context"

type Repository interface {
    Create(ctx context.Context, t *Transfer) error
    GetByID(ctx context.Context, id string) (*Transfer, error)
    UpdateStatus(ctx context.Context, id string, status Status) error
    GetByIdempotencyKey(ctx context.Context, key string) (*Transfer, error)
}
```

```go
// internal/domain/ledger/repository.go
package ledger

import "context"

// Both repos share a unit-of-work (transaction) so they live in the same domain.
type LedgerRepository interface {
    InsertDoubleEntry(ctx context.Context, debit, credit *Entry) error
}

type BalanceRepository interface {
    GetBalance(ctx context.Context, walletID string) (*Balance, error)
    // uses SELECT … FOR UPDATE row lock + UPDATE atomically
    DebitAndCredit(ctx context.Context, fromID, toID string, amount int64) error
}
```

### 2. Extensibility — Adding a New Storage Backend

To add **Redis** as a read-cache for balances:

1. Create `internal/platform/redis/balance_repo.go` implementing `ledger.BalanceRepository`.
2. Create a **caching decorator** (adapter pattern):

```go
// internal/platform/cache/balance_repo.go
type CachedBalanceRepo struct {
    primary ledger.BalanceRepository   // postgres
    cache   ledger.BalanceRepository   // redis
}

func (r *CachedBalanceRepo) GetBalance(ctx context.Context, id string) (*ledger.Balance, error) {
    if b, err := r.cache.GetBalance(ctx, id); err == nil {
        return b, nil
    }
    b, err := r.primary.GetBalance(ctx, id)
    if err == nil {
        _ = r.cache.SetBalance(ctx, id, b) // warm cache, fire-and-forget
    }
    return b, err
}
```

3. Wire the decorated repo in `server/main.go`. **Zero changes to any domain service or handler.**

To add **SQLite** (e.g., for tests or embedded mode):

1. Create `internal/platform/sqlite/` with the same repo implementations.
2. Swap the concrete type in `main.go`. Domain code is untouched.

### 3. Unit of Work — Transactional Transfer Workflow

The `TransferService` orchestrates across domain boundaries within a **single DB transaction**. This is the one place where a transaction coordinator is needed:

```go
// internal/domain/transfer/service.go
type Service struct {
    transferRepo Repository
    ledgerSvc    *ledger.Service   // injected
    walletRepo   wallet.Repository // injected for validation
    txStarter    TxStarter         // interface: Begin() (Tx, error)
}

func (s *Service) Execute(ctx context.Context, req *CreateRequest) (*Transfer, error) {
    // 1. GetByIdempotencyKey(txnId) — if found, return existing transfer (no-op)
    // 2. Begin transaction
    // 3. Validate wallets exist and are active
    // 4. ledgerSvc.DebitAndCredit() — SELECT FOR UPDATE, update balances
    // 5. ledgerSvc.InsertDoubleEntry() — write debit + credit ledger rows
    // 6. Create transfer record with status PROCESSED
    //    (UNIQUE constraint on txn_id catches any race on duplicate insert)
    // 7. Commit
}
```

`TxStarter` is an interface, so test doubles can replace it without a real DB.

### 4. Handler → Service → Repository dependency direction

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
[platform/postgres/X_repo.go]  — concrete impl, imports pgx
```

Dependencies always point **inward**. Platform packages import domain interfaces; domains never import platform.

---

## Comparison: Current vs Proposed

| Aspect | Current (flat) | Proposed (domain) |
|---|---|---|
| Structure | `internal/{db,handler,router,service}` | `internal/domain/{wallet,transfer,ledger}` |
| DB coupling | `service` imports `db` (GORM) directly | Domain imports only its own interface |
| Add new store | Refactor service layer | Add `platform/newstore/` + rewire main |
| Add new domain | Touch shared service file | Create new `domain/X/` package |
| Test isolation | Needs real DB or GORM mock | Mock the interface, zero DB needed |
| Idempotency | Not implemented | UNIQUE constraint on `txn_id` + `GetByIdempotencyKey` on `TransferRepository` |

---

## Verification Plan

### Automated Tests
```bash
# Unit tests (no DB required — interface mocks)
go test ./internal/domain/...

# Integration tests (requires running Postgres)
go test ./tests/... -tags integration

# Race detector — concurrency safety
go test -race ./tests/...
```

### Manual Verification
- `POST /transfers` returns idempotent result on duplicate `txnId`
- `GET /wallet/balance/:id` reflects correct balance after concurrent transfers
- Ledger entries always appear in pairs (debit + credit) for every transfer

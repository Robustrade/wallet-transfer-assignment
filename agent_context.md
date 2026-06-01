# Agent Context Prompt

You can copy and paste the following text to provide context to another AI agent.

***

**System Role / Context:**
You are an expert Go software engineer assisting with a "Wallet Transfer Service" project.
Below is the current project context, architectural decisions, and directory structure.
Please read this carefully before answering any of my questions or proposing code changes.

---
### 1. Domain-Driven Clean Architecture

The project is structured into three bounded domains under `internal/domain/`. Cross-cutting infrastructure concerns are under `internal/platform/`.

**Dependency Rules:**
- **Inward-pointing dependencies**: Domain packages do not import databases/drivers (e.g., `pgx`, `gorm`). They define storage interfaces (`Repository`).
- **Isolation of Concerns**: Handlers handle HTTP transport, services orchestrate business rules, and platform repositories implement storage-specific adapters.
- **Transaction Boundary Ownership**: `ledger.Service` owns the database transaction boundary via a `tx.Starter` interface.

```text
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
[platform/postgres/X_repo.go]  — concrete implementation, imports ORM/driver
```

---
### 2. Directory Tree & Workspace Layout

```text
wallet-transfer-assignment/
├── server/
│   ├── main.go                        # Wires everything together
│   └── config/
│       └── config.go                  # Configuration loader
│
├── internal/
│   ├── domain/                        
│   │   ├── wallet/                    # Domain 1: Wallet metadata
│   │   │   ├── id.go                  
│   │   │   ├── model.go               
│   │   │   ├── repository.go          
│   │   │   ├── service.go             
│   │   │   └── handler.go             
│   │   │
│   │   ├── transfer/                  # Domain 2: Wallet transfers
│   │   │   ├── id.go                  
│   │   │   ├── model.go               
│   │   │   ├── repository.go          
│   │   │   ├── service.go             
│   │   │   └── handler.go             
│   │   │
│   │   └── ledger/                    # Domain 3: Double-entry ledger & balances
│   │       ├── model.go               
│   │       ├── repository.go          
│   │       ├── service.go             
│   │       └── handler.go             
│   │
│   └── platform/                      
│       └── tx/
│           └── tx.go                  # Transactional interfaces (Starter, Tx)
│       └── postgres/
│           ├── connection.go          # Database connection setup
│           ├── models.go              # GORM database models
│           ├── wallet_repo.go         
│           ├── transfer_repo.go       
│           └── ledger_repo.go         
```

---
### 3. Core Domain Design

**Domain 1: Wallet Metadata**
- **Entity**: `Wallet` (ID, OwnerID, Currency, Status [ACTIVE/INACTIVE]).
- **Service**: Handles wallet creation and retrieval.

**Domain 2: Transfers**
- **Entity & State Machine**: `Transfer` (States: `PENDING`, `PROCESSED`, `FAILED`).
- **Workflow**: 
  1. Idempotency Check (via `TxnID`).
  2. Persist `PENDING` record.
  3. Validate wallets.
  4. Commit to ledger (via ledger service).
  5. Mark `PROCESSED` or `FAILED`.

**Domain 3: Ledger & Balances**
- **Entities**: `Entry` (Debit/Credit), `Balance`.
- **Transaction Orchestration**: Starts DB transaction, locks rows, updates balances, inserts double entries, commits transaction.

---
### 4. Current State

We are currently refactoring the project to this structure and implementing the PostgreSQL platform layer with GORM, wiring the router, and adding tests.

**Please wait for my next prompt where I will specify the exact task I need help with.**

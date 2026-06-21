# Wallet Transfer Service — Design Document

---

## Part 1: High-Level Design (HLD)

### 1.1 System Overview

A monolithic REST service enabling atomic wallet-to-wallet money transfers with idempotency guarantees, double-entry ledger accounting, and pessimistic concurrency control.

```mermaid
graph TB
    Client[Client / API Consumer]

    subgraph Application["Spring Boot Application (Port 8080)"]
        Controller[TransferController]
        Service[TransferService]
        Query[WalletQueryService]
        ExHandler[GlobalExceptionHandler]
    end

    subgraph Database["PostgreSQL 16"]
        Wallets[(wallets)]
        Transfers[(transfers)]
        Ledger[(ledger_entries)]
    end

    Client -->|POST /transfers| Controller
    Client -->|GET /transfers/:id| Controller
    Client -->|GET /wallets/:id| Controller
    Controller --> Service
    Controller --> Query
    Controller --> ExHandler
    Service --> Wallets
    Service --> Transfers
    Service --> Ledger
    Query --> Wallets
    Query --> Transfers
```

### 1.2 Technology Stack

| Layer | Technology | Rationale |
|-------|-----------|-----------|
| Runtime | Java 21 | Modern LTS, pattern matching, records |
| Framework | Spring Boot 3.3.5 | Industry standard, mature ecosystem |
| ORM | Hibernate / JPA | Entity mapping, pessimistic lock support |
| Database | PostgreSQL 16 | ACID, CHECK constraints, advisory locks |
| Migrations | Flyway | Versioned schema evolution |
| Testing | Testcontainers + JUnit 5 | Real PostgreSQL in tests, no mocks |
| Build | Maven | Standard Java build |
| Containerization | Docker Compose | Local development PostgreSQL |

### 1.3 API Design

```mermaid
sequenceDiagram
    participant C as Client
    participant API as POST /transfers

    C->>API: { idempotencyKey, fromWalletId, toWalletId, amount }

    alt New transfer - success
        API-->>C: 201 Created + Location header
    else Duplicate request (same key, same payload)
        API-->>C: 200 OK (existing result)
    else Insufficient funds
        API-->>C: 422 Unprocessable Entity
    else Idempotency key reused with different payload
        API-->>C: 409 Conflict
    else Wallet not found
        API-->>C: 404 Not Found
    else Validation failure
        API-->>C: 400 Bad Request
    else Lock timeout (high contention)
        API-->>C: 503 Service Unavailable + Retry-After
    end
```

### 1.4 Key Design Decisions

| Decision | Choice | Alternative Considered |
|----------|--------|----------------------|
| Locking strategy | Pessimistic (`SELECT FOR UPDATE`) | Optimistic (`@Version` + retry) — rejected due to high retry cost under contention |
| Transaction control | Explicit `TransactionTemplate` | Declarative `@Transactional` — rejected because race recovery requires catching exceptions outside TX |
| Money representation | `BIGINT` (minor units) | `BigDecimal` / `NUMERIC` — BIGINT is simpler, faster, and sufficient for integer-unit currencies |
| Idempotency scope | Per-key, permanent (even for FAILED) | TTL-based expiry — simpler, prevents accidental re-execution |
| Architecture style | Monolith | Microservices — unnecessary for single-domain service |
| Seed data | Flyway repeatable migrations (`R__`) | Versioned `V100` — repeatable avoids version collision risk |

### 1.5 Non-Functional Characteristics

| Aspect | Design |
|--------|--------|
| Consistency | Strong — single PostgreSQL with SERIALIZABLE-equivalent via row locks |
| Availability | Single-instance; lock timeout returns 503 with Retry-After for graceful degradation |
| Durability | PostgreSQL WAL + fsync defaults |
| Auditability | Immutable double-entry ledger; failed transfers record balance snapshot |
| Testability | Testcontainers for real DB; no mocking of persistence layer |

---

## Part 2: Low-Level Design (LLD)

### 2.1 Package Structure

```mermaid
graph LR
    subgraph com.wallet.transfer
        controller
        service
        domain
        dto
        repository
        exception
    end

    controller -->|uses| service
    controller -->|uses| dto
    service -->|uses| domain
    service -->|uses| repository
    service -->|uses| exception
    dto -->|maps from| domain
    repository -->|persists| domain
```

| Package | Responsibility |
|---------|---------------|
| `controller` | HTTP layer — request validation, response mapping, status codes |
| `service` | Business logic — transfer orchestration, idempotency, locking |
| `domain` | JPA entities with domain behavior (debit/credit/state transitions) |
| `dto` | Request/response records with Bean Validation annotations |
| `repository` | Spring Data JPA interfaces with custom lock queries |
| `exception` | Domain exceptions + centralized `@RestControllerAdvice` mapping |

### 2.2 Entity-Relationship Diagram

```mermaid
erDiagram
    wallets {
        VARCHAR_64 id PK
        BIGINT balance "NOT NULL, CHECK >= 0"
        TIMESTAMPTZ created_at "NOT NULL"
        TIMESTAMPTZ updated_at "NOT NULL"
    }

    transfers {
        VARCHAR_64 id PK "UUID"
        VARCHAR_128 idempotency_key UK "UNIQUE, NOT NULL"
        VARCHAR_64 from_wallet_id FK "NOT NULL"
        VARCHAR_64 to_wallet_id FK "NOT NULL"
        BIGINT amount "NOT NULL, CHECK > 0"
        VARCHAR_16 status "PENDING → PROCESSED | FAILED"
        BIGINT failure_balance "nullable, internal audit"
        TIMESTAMPTZ created_at "NOT NULL"
        TIMESTAMPTZ updated_at "NOT NULL"
    }

    ledger_entries {
        BIGSERIAL id PK
        VARCHAR_64 wallet_id FK "NOT NULL"
        VARCHAR_64 transfer_id FK "NOT NULL"
        VARCHAR_8 entry_type "DEBIT | CREDIT"
        BIGINT amount "NOT NULL, CHECK > 0"
        TIMESTAMPTZ created_at "NOT NULL"
    }

    wallets ||--o{ transfers : "from_wallet_id"
    wallets ||--o{ transfers : "to_wallet_id"
    wallets ||--o{ ledger_entries : "wallet_id"
    transfers ||--|| ledger_entries : "DEBIT entry"
    transfers ||--|| ledger_entries : "CREDIT entry"
```

### 2.3 Transfer State Machine

```mermaid
stateDiagram-v2
    [*] --> PENDING : Transfer created
    PENDING --> PROCESSED : Balance sufficient, debit+credit committed
    PENDING --> FAILED : Insufficient funds
    PROCESSED --> [*]
    FAILED --> [*]

    note right of PENDING
        Initial state after INSERT.
        Locks held on both wallets.
    end note

    note right of FAILED
        failure_balance recorded.
        Idempotency key permanently consumed.
    end note
```

### 2.4 Core Transfer Flow (Detailed)

```mermaid
flowchart TD
    A[Client: POST /transfers] --> B[Bean Validation]
    B -->|Invalid| B1[400 Bad Request]
    B -->|Valid| C{fromWalletId == toWalletId?}
    C -->|Yes| C1[400: Self-transfer rejected]
    C -->|No| D[Fast-path: findByIdempotencyKey]

    D -->|Found| E{Payload matches?}
    E -->|No| E1[409 CONFLICT]
    E -->|Yes, PROCESSED| E2[200 OK: return existing]
    E -->|Yes, FAILED| E3[422: Insufficient funds]

    D -->|Not found| F[TransactionTemplate.execute]

    subgraph TX["Database Transaction"]
        F --> G{Wallets exist?}
        G -->|No| G1[404: Wallet not found]
        G -->|Yes| H[Lock wallets in ID order]
        H --> I[INSERT transfer PENDING]
        I -->|UNIQUE violation| J[Race: idempotency conflict]
        I -->|OK| K{balance >= amount?}
        K -->|No| K1[Mark FAILED + commit]
        K -->|Yes| L[Debit source wallet]
        L --> M[Credit dest wallet]
        M --> N[INSERT DEBIT ledger entry]
        N --> O[INSERT CREDIT ledger entry]
        O --> P[Mark PROCESSED + commit]
    end

    J --> Q[Lookup winner's transfer]
    Q --> E

    K1 --> R[422: Insufficient funds]
    P --> S[201 Created + Location]
```

### 2.5 Concurrency Control — Detailed Sequence

```mermaid
sequenceDiagram
    participant R1 as Request 1 (A→B, 500)
    participant R2 as Request 2 (A→C, 300)
    participant DB as PostgreSQL

    Note over R1,R2: wallet_A has balance=1000

    R1->>DB: BEGIN
    R2->>DB: BEGIN

    R1->>DB: SELECT wallet_A FOR UPDATE → locked ✓
    R2->>DB: SELECT wallet_A FOR UPDATE → BLOCKED ⏳

    Note over R1: wallet_A < wallet_B, so A locked first
    R1->>DB: SELECT wallet_B FOR UPDATE → locked ✓
    R1->>DB: INSERT transfer (PENDING)
    R1->>DB: UPDATE wallet_A SET balance = 500
    R1->>DB: UPDATE wallet_B SET balance += 500
    R1->>DB: INSERT ledger DEBIT + CREDIT
    R1->>DB: UPDATE transfer SET status = PROCESSED
    R1->>DB: COMMIT ✓

    Note over R2: Lock released, R2 proceeds
    R2->>DB: SELECT wallet_A FOR UPDATE → balance=500 ✓
    R2->>DB: SELECT wallet_C FOR UPDATE → locked ✓
    R2->>DB: INSERT transfer (PENDING)
    R2->>DB: 500 >= 300? Yes → UPDATE wallet_A SET balance = 200
    R2->>DB: UPDATE wallet_C SET balance += 300
    R2->>DB: INSERT ledger DEBIT + CREDIT
    R2->>DB: COMMIT ✓

    Note over R1,R2: Final: A=200, B=+500, C=+300 ✓
```

### 2.6 Idempotency Race Resolution

```mermaid
sequenceDiagram
    participant C1 as Client Retry 1
    participant C2 as Client Retry 2
    participant Svc as TransferService
    participant DB as PostgreSQL

    Note over C1,C2: Same idempotency_key, same payload

    C1->>Svc: executeTransfer(key=abc)
    C2->>Svc: executeTransfer(key=abc)

    Svc->>DB: SELECT * FROM transfers WHERE idempotency_key='abc'
    DB-->>Svc: null (both pass fast-path)

    par Concurrent transactions
        C1->>DB: BEGIN → INSERT transfer(key=abc) → SUCCESS
    and
        C2->>DB: BEGIN → INSERT transfer(key=abc) → UNIQUE VIOLATION
    end

    C1->>DB: ... execute transfer ... COMMIT ✓

    Note over C2: Catches DataIntegrityViolationException OUTSIDE TX
    C2->>DB: SELECT * FROM transfers WHERE idempotency_key='abc'
    DB-->>C2: Transfer(status=PROCESSED)
    C2-->>C2: Validate payload matches → return existing result

    Note over C1,C2: Both clients get identical 201/200 response
```

### 2.7 Class Responsibilities

```mermaid
classDiagram
    class TransferController {
        +createTransfer(TransferRequest) ResponseEntity
        +getTransfer(String) ResponseEntity
        +getWallet(String) ResponseEntity
    }

    class TransferService {
        +executeTransfer(key, from, to, amount) TransferResult
        -performTransfer(key, from, to, amount) Transfer
        -handleExistingTransfer(Transfer) TransferResult
        -validatePayloadMatch(Transfer, from, to, amount)
        -lockWallet(walletId) Wallet
        -isIdempotencyKeyViolation(ex) boolean
    }

    class WalletQueryService {
        +getTransfer(id) Optional~Transfer~
        +getWallet(id) Optional~Wallet~
    }

    class Wallet {
        -String id
        -Long balance
        +debit(amount) void
        +credit(amount) void
    }

    class Transfer {
        -String id
        -String idempotencyKey
        -TransferStatus status
        +markProcessed() void
        +markFailed(balance) void
    }

    class LedgerEntry {
        -String walletId
        -String transferId
        -LedgerEntryType entryType
        -Long amount
    }

    class GlobalExceptionHandler {
        +handleInsufficientFunds() 422
        +handleWalletNotFound() 404
        +handleIdempotencyConflict() 409
        +handlePessimisticLockTimeout() 503
        +handleValidation() 400
    }

    TransferController --> TransferService
    TransferController --> WalletQueryService
    TransferService --> WalletRepository
    TransferService --> TransferRepository
    TransferService --> LedgerEntryRepository
    WalletQueryService --> WalletRepository
    WalletQueryService --> TransferRepository
```

### 2.8 Error Handling Matrix

```mermaid
graph LR
    subgraph Exceptions
        A[InsufficientFundsException]
        B[WalletNotFoundException]
        C[IdempotencyKeyConflictException]
        D[InvalidTransferRequestException]
        E[BalanceViolationException]
        F[PessimisticLockingFailureException]
        G[MethodArgumentNotValidException]
    end

    subgraph HTTP Status
        H[422 Unprocessable]
        I[404 Not Found]
        J[409 Conflict]
        K[400 Bad Request]
        L[503 Service Unavailable]
    end

    A --> H
    B --> I
    C --> J
    D --> K
    E --> H
    F --> L
    G --> K
```

| Exception | HTTP | Error Code | When |
|-----------|------|-----------|------|
| `InsufficientFundsException` | 422 | `INSUFFICIENT_FUNDS` | Source wallet balance < amount |
| `WalletNotFoundException` | 404 | `WALLET_NOT_FOUND` | Wallet ID doesn't exist |
| `IdempotencyKeyConflictException` | 409 | `IDEMPOTENCY_KEY_CONFLICT` | Key reused with different payload |
| `InvalidTransferRequestException` | 400 | `BAD_REQUEST` | Self-transfer attempt |
| `BalanceViolationException` | 422 | `INSUFFICIENT_FUNDS` | Domain guard (defense-in-depth) |
| `PessimisticLockingFailureException` | 503 | `LOCK_TIMEOUT` | Lock wait exceeded 5s |
| `MethodArgumentNotValidException` | 400 | `VALIDATION_ERROR` | Bean Validation failure |

### 2.9 Request/Response DTOs

```mermaid
classDiagram
    class TransferRequest {
        +String idempotencyKey "@NotBlank, @Size(max=128), @Pattern"
        +String fromWalletId "@NotBlank, @Size(max=64), @Pattern"
        +String toWalletId "@NotBlank, @Size(max=64), @Pattern"
        +Long amount "@NotNull, @Positive"
    }

    class TransferResponse {
        +String id
        +String fromWalletId
        +String toWalletId
        +Long amount
        +TransferStatus status
        +String failureReason "null unless FAILED"
        +Instant createdAt
    }

    class WalletResponse {
        +String id
        +Long balance
        +Instant createdAt
        +Instant updatedAt
    }

    class ErrorResponse {
        +String code
        +String message
    }

    TransferRequest ..> TransferResponse : "POST /transfers →"
```

### 2.10 Database Indexes & Performance

```mermaid
graph TD
    subgraph Indexes
        I1[idx_transfers_from_wallet]
        I2[idx_transfers_to_wallet]
        I3[idx_transfers_status]
        I4[idx_ledger_entries_wallet]
        I5[idx_ledger_entries_transfer]
        I6[uq_idempotency_key - UNIQUE]
        I7[uq_ledger_transfer_wallet_type - UNIQUE]
    end

    subgraph Queries
        Q1[Find transfers by sender]
        Q2[Find transfers by recipient]
        Q3[Filter by status]
        Q4[Wallet transaction history]
        Q5[Ledger for a transfer]
        Q6[Idempotency lookup]
        Q7[Duplicate ledger prevention]
    end

    I1 --> Q1
    I2 --> Q2
    I3 --> Q3
    I4 --> Q4
    I5 --> Q5
    I6 --> Q6
    I7 --> Q7
```

### 2.11 Deployment Architecture

```mermaid
graph TD
    subgraph Development
        Dev[Spring Boot + dev profile]
        DevDB[Docker Compose PostgreSQL]
        Seed[R__seed_wallets.sql]
        Dev --> DevDB
        Seed -.->|dev profile only| DevDB
    end

    subgraph Testing
        Test[JUnit 5 + Spring Boot Test]
        TC[Testcontainers PostgreSQL]
        Test --> TC
    end

    subgraph Production
        App[Spring Boot JAR]
        ProdDB[PostgreSQL 16]
        Flyway[Flyway versioned migrations only]
        App --> ProdDB
        Flyway --> ProdDB
    end
```

---

## Summary

| Aspect | HLD | LLD |
|--------|-----|-----|
| Architecture | Monolithic Spring Boot + PostgreSQL | Package-per-layer with rich domain entities |
| Concurrency | Pessimistic row locks, deterministic ordering | `SELECT FOR UPDATE` via JPA, 5s timeout, ID-ordered acquisition |
| Idempotency | Client-supplied key, UNIQUE constraint | Fast-path read → TX insert → race recovery outside TX |
| Accounting | Double-entry ledger | DEBIT+CREDIT pair per transfer, UNIQUE constraint prevents duplicates |
| Error handling | Typed exceptions → HTTP status codes | `@RestControllerAdvice` with granular exception mappers |
| Testing | Real database, no mocks | Testcontainers PostgreSQL, concurrent test harness |

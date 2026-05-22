# Wallet Transfer Service

A robust, highly concurrent wallet-to-wallet transfer service built in Golang. This service is designed to handle strict transactional guarantees, prevent double-spending, maintain a perfect double-entry ledger, and provide exactly-once semantics via a two-tiered idempotency system.

For a deep dive into the architecture, database schema, concurrency strategy, and tradeoffs, please read the [Architecture & Design Document](DESIGN.md).

## Core Features

- **Exactly-Once Semantics:** Dual-tier idempotency using a fast-path in-memory cache and a strict PostgreSQL `UNIQUE` constraint fallback.
- **Concurrency Safe:** Utilizes Pessimistic Row-Level Locking (`SELECT ... FOR UPDATE`) with Lexicographical Lock Ordering to prevent deadlocks and race conditions.
- **Double-Entry Ledger:** Enforces a perfectly balanced, insert-only ledger using PostgreSQL triggers to prevent modifications or deletions.
- **Clean Architecture:** Strict separation of concerns (Handlers -> Controllers -> Repositories).

## Technology Stack

- **Language:** Golang (1.22+)
- **HTTP Framework:** Gin Gonic (for high-performance routing and middleware)
- **Database:** PostgreSQL (for ACID compliance and row-level locking)
- **ORM/Query Builder:** GORM (for structured database interactions and check constraint management)
- **Containerization:** Docker & Docker Compose

## Project Structure

The project follows a modular, layered architecture to ensure separation of concerns and maintainability.

```text
wallet-transfer-assignment/
├── cmd/
│   ├── seed/               # Database seeding script
│   └── server/             # Application entry point
├── config/                 # Configuration files (e.g., .env)
├── internal/
│   ├── api/
│   │   ├── requests/       # Request DTOs
│   │   ├── responses/      # Response DTOs
│   │   └── routes/         # Route definitions and registration
│   ├── controller/         # Business logic & domain-specific validations
│   ├── models/             # GORM models (Domain entities)
│   ├── handlers/           # HTTP Request handlers
│   ├── middlewares/        # Gin middlewares (e.g., Idempotency Interceptor)
│   ├── repositories/       # Database CRUD and transaction management
│   ├── services/
│   │   ├── cache/          # Idempotency cache service
│   │   └── database/       # Database connection & initialization
│   ├── tests/              # Testing suite
│   │   ├── unit/           # Mock-based unit tests
│   │   └── integration/    # Testcontainers-based integration tests
│   │       ├── behavioral/ # Scenario-based logic tests
│   │       ├── concurrency/# Locking and race condition tests
│   │       └── shared/     # Setup and helper utilities
│   └── utils/              # Standard utility static methods
├── docker-compose.yml      # Infrastructure orchestration
├── go.mod                  # Dependency management
└── DESIGN.md               # Architecture documentation
```

### Folder Responsibilities:

- `**cmd/**`: Contains the main execution points. The server starts from `cmd/server/main.go`.
- `**internal/api/**`: Manages the API contract. `requests` and `responses` define the data shapes, while `routes` wires endpoints to handlers.
- `**internal/controller/**`: This is the "Business Layer". It contains the core domain logic, such as orchestrating transfers and generating IDs. It includes `validations.go` for basic pre-flight checks.
- `**internal/handlers/**`: The "Adapter Layer". Handlers parse incoming requests, call the appropriate controller methods, and format the results using the `utils` response helpers.
- `**internal/middlewares/**`: Contains the **Idempotency Response Interceptor**. This middleware transparently handles request deduplication, response caching, and error rollbacks.
- `**internal/repositories/`**: The "Persistence Layer". It handles all direct database interactions, including the critical `ProcessTransfer` method which manages row-level locks and transactional integrity.
- `**internal/services/`**: Provides infrastructure-level services like the in-memory `cache` for idempotency and `database` connectivity.
- `**internal/models/`**: Defines the shared domain models used across all layers of the application.
- `**internal/tests/`**: Contains the dual-tier test suite. `unit/` covers isolated logic via mocks, while `integration/` verifies behavioral correctness and concurrency safety against a real PostgreSQL container.
- `internal/utils/`: Contains the standardized methods which can be used anywhere.

## Setup & Execution Guide

### Prerequisites

- **Go** (1.23+)
- **Docker & Docker Desktop** (Required for the local PostgreSQL instance and Testcontainers during integration tests).

### Environment Configuration

The application requires environment variables for database connectivity and server settings. 

1. Navigate to the `config/` directory.
2. Copy the example environment file:
  ```bash
   cp config/.env.example config/.env
  ```
3. Open `config/.env` and adjust the values (DB credentials, ports, etc.) to match your local setup.

### Infrastructure Setup

Start the local PostgreSQL database in the background using Docker Compose:

```bash
# Using Makefile
make db-up

# Or manually
docker-compose up -d
```

### Code Execution

Start the main Gin server (defaults to port 8080):

```bash
# Using Makefile
make run

# Or manually
go run cmd/server/main.go
```

*(Optional)* If you need to seed the database with initial wallets for manual API testing:

```bash
# Using Makefile
make seed

# Or manually
go run cmd/seed/main.go
```

### Test Execution

The project uses a strict dual-tier testing strategy. **Ensure Docker is running**, as the integration suite spins up ephemeral PostgreSQL instances via Testcontainers.

**Run Unit Tests (Fast, Mock-based):**

```bash
# Using Makefile
make test-unit

# Or manually
go test -v ./internal/tests/unit/...
```

**Run Integration Tests (Real Database, Behavioral & Concurrency Constraints):**

```bash
# Using Makefile
make test-integration

# Or manually
go test -v ./internal/tests/integration/...
```

**Run All Tests:**

```bash
make test
```

## API & Event Contract

### Generate Idempotency Key

**Endpoint:** `POST /generate-id`  
**Purpose:** Generates a unique key for the client to use in a subsequent transfer request, registering it in the fast-path middleware cache.  
**Response (200 OK):**

```json
{"data":{"id":"uuid-v4-string"},"success":true}
```

### Create Transfer

**Endpoint:** `POST /transfers`  
**Headers:** `Idempotency-Key: <uuid-v4-string>`  
**Request Body:**

```json
{
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

**Response (200 OK for Idempotent Hit):**

```json
{
  "transferId": "T1",
  "status": "PROCESSED" // or FAILED
}
```

## Future Scope

- **Database Migrations:** Currently, the application uses GORM's `AutoMigrate` for schema initialization. A future enhancement will introduce a formal migration tool (like `golang-migrate/migrate` or `goose`) to manage versioned, raw SQL schema changes safely across environments.
- **Authentication:** Adding centralized JWT/API key validation.
- **Distributed Cache:** Transitioning the Tier 1 in-memory cache to Redis for multi-instance deployments.


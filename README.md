# Wallet Transfer Service

A production-grade, transactionally consistent financial backend built in **Go** and **PostgreSQL** that supports reliable wallet-to-wallet transfers.

It guarantees **exactly-once API execution** (durable idempotency), **double-entry ledger balancing**, and **concurrency safety** (prevention of double-spending and deadlocks under high write pressure).

---

## 🌟 Key Features

- **Transaction Safety**: Atomic execution of balance mutations, ledger records, and idempotency states inside PostgreSQL transactions.
- **Concurrency Control**: Deterministic lexicographical wallet UUID row-level locking (`SELECT FOR UPDATE ORDER BY id`) preventing deadlocks and race conditions.
- **Durable Idempotency**: Database-backed `idempotency_records` table preserving HTTP status and response payload with SHA-256 payload fingerprinting.
- **Double-Entry Ledger**: Every transfer creates exactly two entries (`DEBIT` and `CREDIT`) ensuring total ledger balance.
- **Monetary Precision**: Uses `NUMERIC(20,4)` in PostgreSQL and `shopspring/decimal` in Go to eliminate floating-point rounding errors.
- **Clean Layered Architecture**: Clear separation across Handler, Service, Repository, and Domain layers.
- **Observability**: Structured JSON logging with `log/slog`, request tracking via `X-Request-ID` header, and health probes (`/health/live`, `/health/ready`).

---

## 🏗️ Architecture & Request Flow

```
HTTP Client ---> Gin Middleware (Request ID, Logging, Recovery)
                     |
                     v
                HTTP Handler (Payload Parsing & Error Mapping)
                     |
                     v
               Transfer Service (Business Logic & Idempotency Check)
                     |
                     v
              PostgreSQL Repository (Row Locking & Atomic Transaction)
                     |
                     v
            PostgreSQL Database (Wallets, Transfers, Ledger, Idempotency)
```

Detailed design notes, transaction boundaries, and state transitions are documented in [docs/design.md](docs/design.md).

---

## 🚀 Quick Start & Local Setup

### Prerequisites

- [Go 1.24+](https://golang.org/doc/install)
- [Docker & Docker Compose](https://docs.docker.com/get-docker/)
- [Make](https://www.gnu.org/software/make/) (optional)

### 1. Running with Docker Compose (Recommended)

Start PostgreSQL and the Go Application:
```bash
docker compose up --build -d
```

Verify service liveness:
```bash
curl http://localhost:8080/health/live
curl http://localhost:8080/health/ready
```

Stop services:
```bash
docker compose down -v
```

### 2. Local Development Execution

1. Start PostgreSQL (e.g. via Docker):
   ```bash
   docker run -d --name postgres_local -e POSTGRES_PASSWORD=postgrespassword -e POSTGRES_DB=wallet_db -p 5432:5432 postgres:16-alpine
   ```
2. Copy environment file:
   ```bash
   cp .env.example .env
   ```
3. Run the service:
   ```bash
   make run
   ```

---

## 🧪 Testing Strategy

The repository includes unit tests, integration tests against a live PostgreSQL database, and concurrent stress tests.

### Run All Tests
```bash
make test
```

### Run Unit Tests
```bash
make test-unit
```

### Run Integration Tests
Make sure PostgreSQL is running at `localhost:5432`, then execute:
```bash
make test-integration
```

### Run Race Detector
```bash
make test-race
```

---

## 📚 API Endpoints Summary

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `POST` | `/transfers` | Create a wallet-to-wallet transfer (supports `idempotencyKey`). |
| `GET` | `/transfers/:id` | Retrieve transfer details and status. |
| `POST` | `/wallets` | Helper endpoint to seed a wallet with balance. |
| `GET` | `/wallets/:id/balance` | Retrieve current wallet balance. |
| `GET` | `/wallets/:id/transactions` | Retrieve double-entry ledger transactions for a wallet. |
| `GET` | `/health/live` | Liveness check probe. |
| `GET` | `/health/ready` | Readiness check probe (verifies DB ping). |

Complete OpenAPI specification is available in [docs/openapi.yaml](docs/openapi.yaml).

---

## 📑 Design & Verification Documentation

- 📄 [docs/design.md](docs/design.md): Deep-dive system design, database schema, concurrency locks, and failure modes.
- 📋 [docs/requirements-checklist.md](docs/requirements-checklist.md): Itemized verification matrix for every requirement in `ASSIGNMENT.md`.
- 🔍 [docs/evaluation-checklist.md](docs/evaluation-checklist.md): Self-assessment against reviewer rubric.
- 🤖 [docs/AI_USAGE.md](docs/AI_USAGE.md): AI tooling disclosure and session transcript information.

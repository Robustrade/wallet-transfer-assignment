# Solution — Wallet Transfer Service (Java)

## Stack

- Java 21, Spring Boot 3.4, Spring Data JPA, Flyway, PostgreSQL (H2 for tests)

## How to Run

```bash
# Option A (default): local H2 DB (no external dependency)
mvn spring-boot:run

# Option B: PostgreSQL via Docker
docker compose up -d
mvn spring-boot:run -Dspring-boot.run.profiles=postgres
```

## How to Test

```bash
mvn test
```

## API

### Create wallet

```http
POST /wallets
{ "id": "wallet_1", "initialBalance": 1000 }
```

### Create transfer

```http
POST /transfers
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

### Get wallet balance

```http
GET /wallets/{walletId}
```

## Schema Design

See [docs/DESIGN.md](docs/DESIGN.md) for tables, constraints, and indexes.

## Idempotency Strategy

- `idempotency_records.idempotency_key` is the primary key.
- Request fingerprint: `SHA-256(from|to|amount)`.
- Duplicate key + same fingerprint → return existing transfer.
- Duplicate key + different fingerprint → `409 Conflict`.
- `transfers.idempotency_key` is also unique as a secondary guard.

## Concurrency Strategy

- Pessimistic write locks on both wallets (`@Lock(PESSIMISTIC_WRITE)`).
- Lock ordering by wallet id to prevent deadlocks.
- All balance changes and ledger inserts in one transaction.

## Tradeoffs

- Stored balances (not derived from ledger) for simpler reads; ledger is the audit trail.
- Failed transfers do not write ledger rows.
- `POST /wallets` is included for seeding/demo; not required by the assignment core endpoint.

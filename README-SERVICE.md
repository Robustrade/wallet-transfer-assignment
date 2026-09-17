# Wallet Transfer Service

Wallet-to-wallet transfers with exactly-once semantics, safe concurrent debits, and a
double-entry ledger. Design rationale lives in [`approach.txt`](./approach.txt).

## Stack

Java 21 · Spring Boot 3.5 · PostgreSQL 16 · Flyway · Testcontainers + JUnit 5

## Run

Everything, including the database:

```bash
docker compose up --build
```

The API is on `http://localhost:8080`. Flyway creates the schema and seeds four wallets on
startup (`wallet_1` … `wallet_4`).

Database only, app from your IDE or the command line:

```bash
docker compose up -d postgres
./mvnw spring-boot:run
```

Connection settings come from `DB_URL`, `DB_USER` and `DB_PASSWORD`, defaulting to
`localhost:5432/wallet` with `wallet`/`wallet`.

## Test

```bash
./mvnw test                     # unit tests only, no Docker needed
./mvnw test -DexcludedGroups=   # everything, including the integration tests
```

Integration tests start a throwaway Postgres via Testcontainers, so Docker must be running.
Row locks, `ON CONFLICT` and the `CHECK` constraints are the things under test and none of
them behave the same on an in-memory database.

## Format and lint

```bash
./mvnw spotless:check     # FORMAT_CHECK_CMD
./mvnw spotless:apply     # fix in place
```

## API

```bash
curl -i -X POST http://localhost:8080/transfers \
  -H 'Content-Type: application/json' \
  -H 'X-Customer-Id: 11111111-1111-1111-1111-111111111111' \
  -d '{
        "idempotencyKey": "abc123",
        "fromWalletId": "wallet_1",
        "toWalletId": "wallet_2",
        "amount": 100
      }'
```

`X-Customer-Id` stands in for the identity the API gateway would pass after authenticating
the caller. Sending the same `idempotencyKey` again returns the original result with an
`Idempotent-Replayed: true` header.

| Outcome | Status |
|---|---|
| transfer processed | `201` |
| replay of an earlier request | same status as the original, plus `Idempotent-Replayed: true` |
| insufficient funds / wallet not active | `422` with a `failureReason` |
| invalid request body | `400` |
| unknown wallet | `404` |
| same key, different request | `409` |
| wallet lock timed out | `503` with `Retry-After` |

## Layout

```text
api/          controller — transport only, no business rules
dto/          request and response bodies, bean validation
exception/    domain exceptions and the HTTP error mapping
service/      transfer workflow and transaction boundary
repository/   Spring Data interfaces, locking queries
entities/     JPA entities and their state transitions
enums/        persisted enum values
resources/db/migration/   Flyway schema and seed data
```

## Reconciling the ledger

Every `PROCESSED` transfer must have exactly one DEBIT and one CREDIT for its amount. That
pairing is guaranteed by the transaction, not by a constraint: `UNIQUE (transfer_id, type)`
stops a transfer being debited twice but cannot require that both rows exist. This query is
the check, and returns no rows on a healthy ledger.

```sql
SELECT t.id, t.amount,
       coalesce(sum(l.amount) FILTER (WHERE l.type = 'DEBIT'),  0) AS debited,
       coalesce(sum(l.amount) FILTER (WHERE l.type = 'CREDIT'), 0) AS credited
FROM transfers t
LEFT JOIN ledger_entries l ON l.transfer_id = t.id
WHERE t.status = 'PROCESSED'
GROUP BY t.id, t.amount
HAVING count(l.id) <> 2
    OR coalesce(sum(l.amount) FILTER (WHERE l.type = 'DEBIT'),  0) <> t.amount
    OR coalesce(sum(l.amount) FILTER (WHERE l.type = 'CREDIT'), 0) <> t.amount;
```

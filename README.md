# Wallet Transfer Service

Spring Boot service for wallet-to-wallet transfers with idempotency, pessimistic locking, and double-entry ledger recording.

## Quick start

```bash
mvn spring-boot:run
```

- **Base URL:** `http://localhost:8080/api/wallet-transfer`
- **Swagger UI:** `http://localhost:8080/api/wallet-transfer/swagger-ui.html`
- **Postman:** `postman/Wallet-Transfer-Service.postman_collection.json`

### PostgreSQL (optional)

```bash
mvn spring-boot:run -Dspring-boot.run.profiles=postgres
```

Configure credentials via `DB_HOST`, `DB_PORT`, `DB_NAME`, `DB_USERNAME`, `DB_PASSWORD`.

## APIs

| Endpoint | Description |
|----------|-------------|
| `POST /v1/transfers` | Create an idempotent wallet transfer |
| `POST /v1/wallet-balance` | Get wallet balance |
| `POST /v1/transfer-history` | Get transfer history for a wallet |

## Architecture

```
controller  →  service  →  repository  →  entity
```

- **Controllers** validate input and delegate to services (no business logic).
- **Services** orchestrate transfers, idempotency, locking, and ledger writes.
- **Repositories** are persistence-only (JPA queries and locks).
- **Entities** define schema constraints, indexes, and safe state transitions.

## Database schema

| Table | Purpose |
|-------|---------|
| `wallets` | Stored balance per wallet (`balance >= 0`, optimistic `@Version`) |
| `transfers` | Transfer workflow (`PENDING` → `PROCESSED` / `FAILED`), unique `idempotency_key` |
| `ledger_entries` | Double-entry DEBIT/CREDIT per successful transfer |
| `idempotency_records` | Cached API responses keyed by `idempotency_key` + request hash |

Key constraints:

- Unique idempotency keys on `transfers` and `idempotency_records`
- Unique `(transfer_id, wallet_id, entry_type)` on ledger entries
- FK from ledger/transfers to wallets
- Check: transfer amount > 0, source ≠ destination, wallet balance ≥ 0

## Idempotency strategy

1. Hash the request payload (`fromWalletId`, `toWalletId`, `amount`).
2. Look up `idempotency_records` by key; if found, verify hash matches and return cached response.
3. Fall back to `transfers` by idempotency key (waits for `PENDING` → terminal state under concurrent duplicates).
4. On new transfer: unique DB constraints prevent duplicate side effects; `DataIntegrityViolationException` triggers a safe replay lookup.

Same key + same payload → original result (success or failure). Same key + different payload → `5003` conflict.

## Concurrency strategy

- **Pessimistic row locks** (`PESSIMISTIC_WRITE`) on source and destination wallets.
- Wallets locked in **sorted wallet ID order** to prevent deadlocks.
- **Stored balance** updated inside the same transaction as ledger entries.
- **Optimistic `@Version`** on wallets as a secondary guard.

## Transfer state machine

```
PENDING → PROCESSED   (successful debit/credit + ledger entries)
PENDING → FAILED      (e.g. insufficient funds; no ledger entries)
```

State transitions are enforced on the `Transfer` entity (`markProcessed`, `markFailed`).

## Testing

```bash
mvn test
```

Behavioral integration tests (`TransferIntegrationTest`) cover:

- Successful transfer and balanced ledger
- Idempotent replay (success and failure)
- Concurrent duplicate idempotency key
- Concurrent double-spend prevention
- Insufficient funds, wallet not found, validation errors

## Assignment docs

- [`ASSIGNMENT.md`](./ASSIGNMENT.md) — problem statement
- [`evaluation_guide.md`](./evaluation_guide.md) — reviewer rubric
- [`.github/copilot-instructions.md`](./.github/copilot-instructions.md) — Copilot PR review focus
- [`.github/pull_request_template.md`](./.github/pull_request_template.md) — PR structure (schema, idempotency, concurrency, AI disclosure)

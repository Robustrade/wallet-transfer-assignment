# Wallet Transfer Solution

Spring Boot backend + Angular frontend for the wallet transfer assignment.

## Documentation

- [`docs/BACKEND_DESIGN.md`](./docs/BACKEND_DESIGN.md) — schema, architecture, idempotency, concurrency
- [`docs/AI_DISCLOSURE.md`](./docs/AI_DISCLOSURE.md) — Cursor usage and PR prompt transcript

## Structure

```text
backend/src/main/java/com/wallettransfer/
  wallet/          domain, dto, repository, service, web
  transfer/        domain, dto, repository, service, web
  ledger/          domain, dto, repository, service
  idempotency/     domain, repository, service
  common/          dto, web (exception handler)
  config/
frontend/          Angular app to test transfers
```

## Run backend

```bash
cd backend
./mvnw spring-boot:run
```

On Windows:

```powershell
cd backend
.\mvnw.cmd spring-boot:run
```

API base: `http://localhost:8080/api`

## Run frontend

```bash
cd frontend
npm install
npm start
```

UI: `http://localhost:4200`

## Test backend

```bash
cd backend
./mvnw test
```

## Main endpoints

- `POST /api/transfers` - create transfer (idempotent)
- `GET /api/wallets` - list wallet balances
- `GET /api/transfers` - transfer history
- `GET /api/transfers/{id}/ledger` - ledger entries for a transfer

## Seed wallets

- `wallet_1` = 1000
- `wallet_2` = 500
- `wallet_3` = 250

## UI features

- View wallet balances
- Create transfers
- Resend the same idempotency key to verify duplicate handling
- Inspect transfer history and ledger entries

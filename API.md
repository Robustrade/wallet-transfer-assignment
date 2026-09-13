# Wallet Transfer API

Base URL: `http://localhost:8080`

All request and response bodies use `application/json`.

## Health Check

```http
GET /actuator/health
```

Response: `200 OK`

```json
{
  "status": "UP"
}
```

The health endpoint verifies that the application is running and can check its configured database connection.

## 1. Create Wallet

```http
POST /wallets
```

Request:

```json
{
  "initialBalance": 1000.00
}
```

Response: `201 Created`

```json
{
  "walletId": "287a573f-6a72-429f-a913-da308f93cce5",
  "balance": 1000.00
}
```

Rules:

- The service generates a unique UUID for `walletId`.
- `initialBalance` must be non-negative and have at most four decimal places.

PowerShell example:

```powershell
Invoke-RestMethod -Method Post -Uri http://localhost:8080/wallets `
  -ContentType 'application/json' `
  -Body '{"initialBalance":1000.00}'
```

Save the returned `walletId`; use it in balance and transfer requests.

## 2. Get Wallet Balance

```http
GET /wallets/{walletId}
```

Example:

```http
GET /wallets/287a573f-6a72-429f-a913-da308f93cce5
```

Response: `200 OK`

```json
{
  "walletId": "287a573f-6a72-429f-a913-da308f93cce5",
  "balance": 900.00
}
```

An unknown wallet returns `404 Not Found`.

```

## 3. Create Transfer

```http
POST /transfers
```

Request:

```json
{
  "idempotencyKey": "transfer-001",
  "fromWalletId": "287a573f-6a72-429f-a913-da308f93cce5",
  "toWalletId": "3e6fb8ae-2ad5-4df3-bf89-c96d5ed3a1b7",
  "amount": 100.00
}
```

Response: `201 Created`

```json
{
  "transferId": "8e4f5d0a-7a7b-4ef3-a1c5-4e1a2d4e8b1c",
  "idempotencyKey": "transfer-001",
  "fromWalletId": "287a573f-6a72-429f-a913-da308f93cce5",
  "toWalletId": "3e6fb8ae-2ad5-4df3-bf89-c96d5ed3a1b7",
  "amount": 100.00,
  "status": "PROCESSED",
  "createdAt": "2026-09-12T12:00:00Z"
}
```


### Idempotency behavior

Repeat the exact same request with `transfer-001`. The service returns the original transfer and does not debit or credit the wallets again.

Reusing `transfer-001` with a different amount or wallet returns `400 Bad Request`.

### Failure responses

- `400 Bad Request`: invalid input, same source and destination, or idempotency-key reuse with different data.
- `404 Not Found`: source or destination wallet does not exist.
- `409 Conflict`: source wallet has insufficient funds.

Error body:

```json
{
  "error": "Insufficient funds in wallet: 287a573f-6a72-429f-a913-da308f93cce5"
}
```

## Local startup

From the project root in PowerShell:

```powershell
# Build the application inside Docker and start PostgreSQL and the API
docker compose up --build -d

# Check service status
docker compose ps
```

The Dockerfile contains a Maven/JDK build stage, so Java and Maven do not need to be installed on the host for this path.

If Java 21 and Maven 3.9+ are installed, you can run PostgreSQL in Docker and the API locally:

```powershell
docker compose up -d postgres
mvn clean verify
mvn spring-boot:run
```

Stop containers:

```powershell
docker compose down
```

The database data is ephemeral because no Docker volume is configured. Removing the PostgreSQL container removes the local database data.
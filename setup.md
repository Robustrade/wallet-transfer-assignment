# Setup Guide — Wallet Transfer Service

This guide provides step-by-step instructions to set up, run, and test the Wallet Transfer Service on your local machine.

---

## 🛠️ Prerequisites

Before running this service, ensure you have the following installed:

1. **Go (Golang)**: Version `1.25.0` or higher.
2. **Docker**: Required to run the local PostgreSQL database and execute integration tests via `testcontainers-go`.
3. **Make**: (Recommended) For executing tasks defined in the [Makefile](file:///wallet-transfer-assignment/Makefile).

---

## ⚙️ Environment Configuration

The application is configured using environment variables. A template file is provided at [.env](file:///wallet-transfer-assignment/.env).

Key environment variables:

| Variable | Description | Default / Recommended Value |
| :--- | :--- | :--- |
| `DATABASE_URL` | The PostgreSQL database connection string | `postgres://postgres:Abc123@localhost:5435/Wallet?sslmode=disable` |
| `PORT` | The HTTP port the service will listen on | `8000` |
| `POSTGRES_PASSWORD`| The password for the Docker Postgres container | `Abc123` |
| `APP_ENV` | The environment name | `development` |

---

## 💾 Database Setup

The database schema is managed automatically at application startup using GORM's `AutoMigrate` feature configured in [server/main.go](file:///wallet-transfer-assignment/server/main.go). 

To spin up a local PostgreSQL instance in Docker matching the configuration:

```bash
# Using Makefile
make docker-db

# Or manually running Docker command
docker run --name wallet-db -e POSTGRES_DB=Wallet -e POSTGRES_PASSWORD=Abc123 -p 5435:5432 -d postgres:15-alpine
```

This will run a PostgreSQL container listening on port `5435`.

To stop the container:
```bash
# Using Makefile
make docker-db-stop

# Or manually
docker stop wallet-db && docker rm wallet-db
```

---

## 🚀 Running the Server

1. Download Go dependencies:
   ```bash
   go mod download
   ```
2. Start both the PostgreSQL database container and the API server:
   ```bash
   # Recommended (Starts the container, waits for initialization, and runs the server)
   make up
   ```
   *Alternatively, start them separately:*
   ```bash
   # 1. Start the Postgres container
   make docker-db

   # 2. Run the API server
   make run
   ```
   *Or run manually:*
   ```bash
   go run server/main.go
   ```

The server will start up and listen on the configured port (default is `8000`). You will see log outputs confirming the server initialization and GORM auto-migrations.

---

## 🧪 Running the Tests

The codebase includes both unit and integration tests.

> [!IMPORTANT]
> Integration and concurrency tests require a running Docker daemon on your host system because they use the `testcontainers-go` library in [tests/integration/testhelper/db.go](file:///wallet-transfer-assignment/tests/integration/testhelper/db.go) to provision an ephemeral PostgreSQL instance.

### Run All Tests
Runs all unit and integration tests:
```bash
# Using Makefile
make test

# Or manually
go test -v ./...
```

### Run Unit Tests Only
Runs only the core business logic unit tests:
```bash
# Using Makefile
make test-unit

# Or manually
go test -v ./tests/wallet/... ./tests/transfer/... ./tests/ledger/...
```

### Run Integration & Concurrency Tests Only
Runs only the integration and concurrency safety suites:
```bash
# Using Makefile
make test-integration

# Or manually
go test -v ./tests/integration/...
```
### Run Tests with Race Detector & Coverage (CI Suite)
Runs the test suite with the race detector and coverage analysis enabled:
```bash
# Using Makefile
make test-ci

# Or manually
go test ./... -race -cover
```

---

## 🔍 Smoke Testing & Verification

Once your server is running on `http://localhost:8000`, you can execute the following sequential requests to verify that the core endpoints are working properly.

### 1. Create Wallets
Create a sender wallet:
```bash
curl -X POST http://localhost:8000/wallets \
  -H "Content-Type: application/json" \
  -d '{"ownerId": "owner_abc", "currency": "USD"}'
```
*Expected Response:*
```json
{
  "data": {
    "ID": "wallet_...",
    "OwnerID": "owner_abc",
    "Currency": "USD",
    "Status": "ACTIVE",
    "CreatedAt": "...",
    "UpdatedAt": "..."
  },
  "error": null,
  "status": 201
}
```
*Make note of the generated sender `"ID"` (e.g., `wallet_123`).*

Create a receiver wallet:
```bash
curl -X POST http://localhost:8000/wallets \
  -H "Content-Type: application/json" \
  -d '{"ownerId": "owner_xyz", "currency": "USD"}'
```
*Make note of the generated receiver `"ID"` (e.g., `wallet_456`).*

### 2. Fund the Sender Wallet
Add funds to the sender wallet using the faucet endpoint:
```bash
curl -X POST http://localhost:8000/transfers/fund \
  -H "Content-Type: application/json" \
  -d '{"toWalletId": "<SENDER_WALLET_ID>", "amount": 10000}'
```
*Expected Response:*
```json
{
  "data": {
    "status": "PROCESSED",
    "txnID": "fund_..."
  },
  "error": null,
  "status": 200
}
```

### 3. Check Sender Balance
Verify that the sender wallet now has a balance of `10000`:
```bash
curl http://localhost:8000/wallet/balance/<SENDER_WALLET_ID>
```
*Expected Response:*
```json
{
  "data": {
    "WalletID": "<SENDER_WALLET_ID>",
    "Balance": 10000,
    "UpdatedAt": "..."
  },
  "error": null,
  "status": 200
}
```

### 4. Execute a Wallet-to-Wallet Transfer
Perform a transfer of `3000` units between the two wallets.
Make sure to generate a unique `txnId` (e.g. UUID) to serve as the idempotency key:
```bash
curl -X POST http://localhost:8000/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "txnId": "tx_unique_id_101",
    "fromWalletId": "<SENDER_WALLET_ID>",
    "toWalletId": "<RECEIVER_WALLET_ID>",
    "amount": 3000
  }'
```
*Expected Response:*
```json
{
  "data": {
    "status": "PROCESSED",
    "txnID": "tx_unique_id_101"
  },
  "error": null,
  "status": 200
}
```

### 5. Verify Idempotency
Resend the exact same payload with the same `txnId`. The server should recognize the duplicate transaction, skip execution, and return the same result without double-deducting:
```bash
curl -X POST http://localhost:8000/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "txnId": "tx_unique_id_101",
    "fromWalletId": "<SENDER_WALLET_ID>",
    "toWalletId": "<RECEIVER_WALLET_ID>",
    "amount": 3000
  }'
```
*Expected Response:* Same as step 4 (the status should be `PROCESSED` and no additional balance is deducted).

### 6. Verify Final Balances
Check both balances to ensure the debit and credit were recorded correctly:

*   Sender Balance (should be `7000`):
    ```bash
    curl http://localhost:8000/wallet/balance/<SENDER_WALLET_ID>
    ```
*   Receiver Balance (should be `3000`):
    ```bash
    curl http://localhost:8000/wallet/balance/<RECEIVER_WALLET_ID>
    ```

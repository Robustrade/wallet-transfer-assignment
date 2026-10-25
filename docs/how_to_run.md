# How to Run the Service

## 1. Quickstart (Zero-Config In-Memory Mode)

The service detects whether PostgreSQL is accessible. If PostgreSQL is not running, it automatically initializes a high-performance, thread-safe in-memory repository. This allows instant execution without setting up external databases or Docker.

```bash
cd /Users/zog/Documents/AntiGravity/Robustrade/wallet-transfer-assignment
go run cmd/server/main.go
```

The server binds to port `8080`:
```text
{"time":"2026-10-25T18:00:00Z","level":"INFO","msg":"starting wallet transfer service on :8080"}
```

---

## 2. Interactive Web Dashboard (UI)

Open your web browser and navigate to:
👉 **`http://localhost:8080/ui`** (or `http://localhost:8080/`)

The web dashboard allows you to visually test and verify:
1. **Wallet Creation:** Create wallets with custom initial balances.
2. **Execute Transfers:** Perform transfers between wallets.
3. **Test Idempotency Replay:** Re-submit the exact same transfer to verify zero double-debits and identical cached response replay.
4. **Ledger Audit & Reconciliation:** One-click audit comparing stored balances against historical ledger entries.
5. **Live Event Stream:** Real-time log of HTTP status codes and responses.

---

## 3. Running with PostgreSQL via Docker Compose

To run the complete production stack (PostgreSQL 16 container + Service container):

```bash
# Build and start both containers
docker compose up --build

# Run in background
docker compose up -d

# Teardown
docker compose down -v
```

---

## 4. API Usage Examples via `curl`

### Health Check
```bash
curl -s http://localhost:8080/health | jq .
```

### Create Wallets
```bash
# Create Wallet A with 10,000 units ($100.00)
curl -s -X POST http://localhost:8080/wallets \
  -H "Content-Type: application/json" \
  -d '{"id":"wallet_a","balance":10000,"currency":"USD"}' | jq .

# Create Wallet B with 5,000 units ($50.00)
curl -s -X POST http://localhost:8080/wallets \
  -H "Content-Type: application/json" \
  -d '{"id":"wallet_b","balance":5000,"currency":"USD"}' | jq .
```

### Execute a Transfer
```bash
curl -s -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "tx-demo-001",
    "fromWalletId": "wallet_a",
    "toWalletId": "wallet_b",
    "amount": 2500,
    "currency": "USD"
  }' | jq .
```

### Verify Idempotent Replay (Same Request)
```bash
curl -s -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "tx-demo-001",
    "fromWalletId": "wallet_a",
    "toWalletId": "wallet_b",
    "amount": 2500,
    "currency": "USD"
  }' | jq .
```
*(Notice: Returns the identical transfer result immediately without deducting funds a second time).*

### Query Wallet Balance & Audit Ledger Reconciliation
```bash
curl -s "http://localhost:8080/wallets/wallet_a?reconcile=true" | jq .
```

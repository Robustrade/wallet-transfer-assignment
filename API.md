# API Contract & Specification

## Base URL

```
http://localhost:8080
```

## Transfer APIs

### 1. Create Transfer

**Endpoint:** `POST /transfers`

**Description:** Create a wallet-to-wallet transfer with idempotent guarantees.

**Request:**
```json
{
  "idempotencyKey": "abc-123-unique-key",
  "fromWalletId": "wallet_uuid_1",
  "toWalletId": "wallet_uuid_2",
  "amount": 10000,
  "description": "Payment for services"
}
```

**Request Fields:**
| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `idempotencyKey` | string | Yes | Unique key for idempotency (UUID recommended) |
| `fromWalletId` | string | Yes | Source wallet UUID |
| `toWalletId` | string | Yes | Destination wallet UUID |
| `amount` | integer | Yes | Amount in cents (must be > 0) |
| `description` | string | No | Optional transfer description |

**Success Response (201 Created):**
```json
{
  "transferId": "transfer_uuid",
  "fromWalletId": "wallet_uuid_1",
  "toWalletId": "wallet_uuid_2",
  "amount": 10000,
  "status": "PROCESSED",
  "createdAt": "2025-01-15T10:30:45Z"
}
```

**Error Responses:**

```json
// 400 Bad Request - Invalid Amount
{
  "code": "INVALID_AMOUNT",
  "message": "Amount must be positive"
}

// 400 Bad Request - Self Transfer
{
  "code": "SELF_TRANSFER",
  "message": "Cannot transfer to the same wallet"
}

// 400 Bad Request - Missing Idempotency Key
{
  "code": "MISSING_IDEMPOTENCY_KEY",
  "message": "Idempotency key is required"
}

// 402 Payment Required - Insufficient Funds
{
  "code": "INSUFFICIENT_FUNDS",
  "message": "Source wallet has insufficient balance",
  "details": {
    "walletId": "wallet_uuid_1",
    "balance": 5000,
    "requested": 10000
  }
}

// 404 Not Found - Wallet Not Found
{
  "code": "WALLET_NOT_FOUND",
  "message": "Wallet not found"
}

// 409 Conflict - Duplicate Transfer
{
  "code": "DUPLICATE_TRANSFER",
  "message": "Transfer with this idempotency key already exists",
  "details": {
    "transferId": "transfer_uuid",
    "createdAt": "2025-01-15T10:30:45Z"
  }
}

// 500 Internal Server Error
{
  "code": "INTERNAL_ERROR",
  "message": "Internal server error"
}
```

**Idempotency Behavior:**

```
First Request:
POST /transfers
{
  "idempotencyKey": "unique-123",
  "fromWalletId": "w1",
  "toWalletId": "w2",
  "amount": 1000
}
Response: 201 Created
{
  "transferId": "t_xyz",
  "status": "PROCESSED"
}

Retry Request (network lost):
POST /transfers
{
  "idempotencyKey": "unique-123",  // Same key
  "fromWalletId": "w1",
  "toWalletId": "w2",
  "amount": 1000
}
Response: 201 Created (same response)
{
  "transferId": "t_xyz",  // Same transfer ID
  "status": "PROCESSED"
}
```

---

### 2. Get Transfer Status

**Endpoint:** `GET /transfers/{transferId}`

**Description:** Retrieve transfer status and details.

**Path Parameters:**
| Parameter | Type | Required | Notes |
|-----------|------|----------|-------|
| `transferId` | string | Yes | Transfer UUID |

**Success Response (200 OK):**
```json
{
  "transferId": "transfer_uuid",
  "fromWalletId": "wallet_uuid_1",
  "toWalletId": "wallet_uuid_2",
  "amount": 10000,
  "status": "PROCESSED",
  "createdAt": "2025-01-15T10:30:45Z",
  "updatedAt": "2025-01-15T10:30:46Z"
}
```

**Error Response (404 Not Found):**
```json
{
  "code": "TRANSFER_NOT_FOUND",
  "message": "Transfer not found"
}
```

---

## Wallet APIs

### 3. Get Wallet

**Endpoint:** `GET /wallets/{walletId}`

**Description:** Retrieve wallet balance and details.

**Path Parameters:**
| Parameter | Type | Required |
|-----------|------|----------|
| `walletId` | string | Yes |

**Success Response (200 OK):**
```json
{
  "walletId": "wallet_uuid",
  "balance": 50000,
  "currency": "USD",
  "createdAt": "2025-01-15T10:00:00Z",
  "updatedAt": "2025-01-15T10:30:45Z"
}
```

**Error Response (404 Not Found):**
```json
{
  "code": "WALLET_NOT_FOUND",
  "message": "Wallet not found"
}
```

---

### 4. Get Wallet Balance

**Endpoint:** `GET /wallets/{walletId}/balance`

**Description:** Quick endpoint to retrieve only balance.

**Success Response (200 OK):**
```json
{
  "walletId": "wallet_uuid",
  "balance": 50000
}
```

---

## Health Check

**Endpoint:** `GET /health`

**Response:**
```json
{
  "status": "ok"
}
```

---

## HTTP Status Codes

| Code | Meaning | Scenarios |
|------|---------|-----------|
| 200 | OK | Successful read operation |
| 201 | Created | Transfer successfully created |
| 400 | Bad Request | Invalid input, validation failed |
| 402 | Payment Required | Insufficient funds |
| 404 | Not Found | Resource doesn't exist |
| 409 | Conflict | Idempotency key conflict |
| 500 | Internal Server Error | Database/system error |

---

## Request Headers

All requests should include:
```
Content-Type: application/json
```

---

## Response Headers

All responses include:
```
Content-Type: application/json
X-Request-ID: unique-id (recommended for tracing)
```

---

## Rate Limiting (Future)

Recommended:
```
X-RateLimit-Limit: 1000
X-RateLimit-Remaining: 999
X-RateLimit-Reset: 1234567890
```

---

## Example Workflow

### Complete Transfer Scenario

```bash
# 1. Create transfer
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "order-12345-transfer",
    "fromWalletId": "seller-wallet",
    "toWalletId": "buyer-wallet",
    "amount": 50000,
    "description": "Payment for order #12345"
  }'

# Response: 201 Created
{
  "transferId": "t_abc123",
  "status": "PROCESSED"
}

# 2. Check transfer status
curl -X GET http://localhost:8080/transfers/t_abc123

# Response: 200 OK
{
  "transferId": "t_abc123",
  "status": "PROCESSED",
  "amount": 50000
}

# 3. Check wallet balances
curl -X GET http://localhost:8080/wallets/seller-wallet
curl -X GET http://localhost:8080/wallets/buyer-wallet

# Response: 200 OK
{
  "walletId": "seller-wallet",
  "balance": 150000,
  "currency": "USD"
}
```

### Retry with Idempotency

```bash
# First request fails due to network timeout
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "unique-request-id",
    "fromWalletId": "w1",
    "toWalletId": "w2",
    "amount": 1000
  }'
# Response: (timeout, no response received)

# Client retries with SAME idempotencyKey
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "unique-request-id",
    "fromWalletId": "w1",
    "toWalletId": "w2",
    "amount": 1000
  }'
# Response: 201 Created (same response as first request)
{
  "transferId": "t_xyz",
  "status": "PROCESSED"
}

# The transfer was NOT created twice ✓
# Idempotency key ensures exactly-once semantics ✓
```

# Wallet Transfer Service


## Running the Application

### Build

```bash
mvn clean install
```

### Run

```bash
mvn spring-boot:run
```

Application starts on:

```text
http://localhost:8080
```

---

## Seed Data

Application creates two wallets on startup.

Example:

```text
Wallet 1
11111111-1111-1111-1111-111111111111

Wallet 2
22222222-2222-2222-2222-222222222222
```

If IDs differ in your environment, check startup logs.

---

# API Documentation

## Create Transfer

### Endpoint

```http
POST /transfers
```

### Request

```json
{
  "idempotencyKey":"transfer-001",
  "fromWalletId":"11111111-1111-1111-1111-111111111111",
  "toWalletId":"22222222-2222-2222-2222-222222222222",
  "amount":100
}
```

---

## Successful Response

```json
{
  "transferId":"e6f4a56f-2ec2-4bd7-8f36-3c0c4dc0f18e",
  "status":"PROCESSED",
  "fromWalletId":"11111111-1111-1111-1111-111111111111",
  "toWalletId":"22222222-2222-2222-2222-222222222222",
  "amount":100
}
```

---

# Testing

## 1. Successful Transfer

```bash
curl -X POST http://localhost:8080/transfers \
-H "Content-Type: application/json" \
-d '{
  "idempotencyKey":"transfer-001",
  "fromWalletId":"11111111-1111-1111-1111-111111111111",
  "toWalletId":"22222222-2222-2222-2222-222222222222",
  "amount":100
}'
```

---

## 2. Duplicate Transfer

Execute the same request again.

```bash
curl -X POST http://localhost:8080/transfers \
-H "Content-Type: application/json" \
-d '{
  "idempotencyKey":"transfer-001",
  "fromWalletId":"11111111-1111-1111-1111-111111111111",
  "toWalletId":"22222222-2222-2222-2222-222222222222",
  "amount":100
}'
```

Expected:

```text
DuplicateTransferException
```

---

## 3. Insufficient Funds

```bash
curl -X POST http://localhost:8080/transfers \
-H "Content-Type: application/json" \
-d '{
  "idempotencyKey":"transfer-002",
  "fromWalletId":"11111111-1111-1111-1111-111111111111",
  "toWalletId":"22222222-2222-2222-2222-222222222222",
  "amount":1000000
}'
```

Expected:

```text
InsufficientFundsException
```

---

## 4. Source Wallet Not Found

```bash
curl -X POST http://localhost:8080/transfers \
-H "Content-Type: application/json" \
-d '{
  "idempotencyKey":"transfer-003",
  "fromWalletId":"99999999-9999-9999-9999-999999999999",
  "toWalletId":"22222222-2222-2222-2222-222222222222",
  "amount":100
}'
```

Expected:

```text
WalletNotFoundException
```

---

## 5. Destination Wallet Not Found

```bash
curl -X POST http://localhost:8080/transfers \
-H "Content-Type: application/json" \
-d '{
  "idempotencyKey":"transfer-004",
  "fromWalletId":"11111111-1111-1111-1111-111111111111",
  "toWalletId":"99999999-9999-9999-9999-999999999999",
  "amount":100
}'
```

Expected:

```text
WalletNotFoundException
```

---

## 6. Same Wallet Transfer

```bash
curl -X POST http://localhost:8080/transfers \
-H "Content-Type: application/json" \
-d '{
  "idempotencyKey":"transfer-005",
  "fromWalletId":"11111111-1111-1111-1111-111111111111",
  "toWalletId":"11111111-1111-1111-1111-111111111111",
  "amount":100
}'
```

Expected:

```text
InvalidTransferException
```

---

## 7. Invalid UUID

```bash
curl -X POST http://localhost:8080/transfers \
-H "Content-Type: application/json" \
-d '{
  "idempotencyKey":"transfer-006",
  "fromWalletId":"11111111-1111-1111-1111-111111111111",
  "toWalletId":"invalid-uuid",
  "amount":100
}'
```

Expected:

```text
400 Bad Request
```

---

## 8. Negative Amount

```bash
curl -X POST http://localhost:8080/transfers \
-H "Content-Type: application/json" \
-d '{
  "idempotencyKey":"transfer-007",
  "fromWalletId":"11111111-1111-1111-1111-111111111111",
  "toWalletId":"22222222-2222-2222-2222-222222222222",
  "amount":-100
}'
```

Expected:

```text
Validation Error
```

---

## Assumptions

* Wallet balances are stored and updated directly.
* SQLite is used for simplicity.
* All transfer operations are executed atomically.
* Every successful transfer creates exactly two ledger entries.
* Duplicate transfers are prevented using idempotency keys.

---

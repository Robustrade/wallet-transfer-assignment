# API Contract — Wallet Transfer Service

Base URL (local): `http://localhost:8080`

All request/response bodies are JSON. All monetary amounts are **integers in minor
units** (e.g. paise for INR) — never floats.

**Response shape rule:** if a request produced or referenced a transfer, the response
body is the transfer resource and the HTTP status code tells you whether it settled. The
`{"error": {...}}` envelope is used only when no transfer resource exists to return
(validation failures, unknown wallet, key reuse, etc).

---

## `POST /transfers`

Create a wallet-to-wallet transfer. Idempotent on `idempotencyKey`.

### Request

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `idempotencyKey` | string | yes* | 1–255 printable ASCII chars. *May instead be supplied via the `Idempotency-Key` header. Exactly one of the two must be present; if both are present they must match. |
| `fromWalletId` | string | yes | must exist, must differ from `toWalletId` |
| `toWalletId` | string | yes | must exist |
| `amount` | integer | yes | minor units, `1 <= amount <= 1_000_000_000_000_000` |
| `currency` | string | no | 3-letter ISO code. If given, must match both wallets' currency. If omitted, inferred from the wallets. |

The body is decoded strictly: unknown fields, non-integer `amount` (e.g. `100.5` or
`"100"`), and bodies over 64 KiB are all rejected as `400 MALFORMED_REQUEST`.

### `curl`

```bash
curl -s -X POST http://localhost:8080/transfers \
  -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
```

### Response — settled (`201`)

```json
{
  "id": "0190d1e7-3a2b-7c11-9d4e-2f6a8b1c0d3e",
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100,
  "currency": "INR",
  "status": "PROCESSED",
  "failureCode": null,
  "failureMessage": null,
  "createdAt": "2026-08-12T10:00:00.123456Z",
  "settledAt": "2026-08-12T10:00:00.234567Z"
}
```

### Response — failed at settlement (`422`)

Same shape, with `status: "FAILED"`, `failureCode: "INSUFFICIENT_FUNDS"`,
`failureMessage` set, and `settledAt: null`.

### Response — replay of either of the above

Identical status code and body to the original response, plus response header:

```
Idempotent-Replay: true
```

### Error responses

```json
{ "error": { "code": "VALIDATION_ERROR", "message": "amount must be greater than zero", "field": "amount" } }
```

| Condition | Status | `code` |
|---|---|---|
| Malformed JSON / unknown field / wrong type / oversized body | 400 | `MALFORMED_REQUEST` |
| Field validation failure (empty key, non-positive amount, same wallet twice, missing/conflicting idempotency key) | 400 | `VALIDATION_ERROR` |
| `fromWalletId` or `toWalletId` does not exist | 404 | `WALLET_NOT_FOUND` |
| Same key, different request payload | 409 | `IDEMPOTENCY_KEY_REUSE` |
| Wallet currencies differ, or requested `currency` differs from the wallets' | 409 | `CURRENCY_MISMATCH` |
| Replay observed a transfer still `PENDING` | 409 | `TRANSFER_IN_PROGRESS` |
| Could not acquire a lock in time | 503 (`Retry-After: 1`) | `RESOURCE_BUSY` |
| Anything unexpected | 500 | `INTERNAL_ERROR` |

---

## `GET /transfers/{id}`

Fetch a transfer by id.

```bash
curl -s http://localhost:8080/transfers/0190d1e7-3a2b-7c11-9d4e-2f6a8b1c0d3e
```

`200` with the transfer resource, or `404 TRANSFER_NOT_FOUND`.

---

## `POST /wallets`

Create a `USER` wallet at a zero balance. *(Optional enhancement from `ASSIGNMENT.md`.)*

### Request

```json
{ "id": "wallet_1", "currency": "INR" }
```

```bash
curl -s -X POST http://localhost:8080/wallets \
  -H 'Content-Type: application/json' \
  -d '{"id":"wallet_1","currency":"INR"}'
```

### Response (`201`)

```json
{ "id": "wallet_1", "kind": "USER", "currency": "INR", "balance": 0, "createdAt": "2026-08-12T10:00:00Z" }
```

`409 WALLET_ALREADY_EXISTS` if the id is taken.

---

## `GET /wallets/{id}`

Fetch a wallet's current balance. *(Optional enhancement.)*

```bash
curl -s http://localhost:8080/wallets/wallet_1
```

`200` with the wallet resource, or `404 WALLET_NOT_FOUND`.

---

## `GET /wallets/{id}/entries?limit=20&beforeId=`

Ledger history for a wallet, newest first, keyset-paginated on `ledger_entries.id`.
*(Optional enhancement.)*

```bash
curl -s 'http://localhost:8080/wallets/wallet_1/entries?limit=20'
```

```json
{
  "entries": [
    {
      "id": 42,
      "transferId": "0190d1e7-3a2b-7c11-9d4e-2f6a8b1c0d3e",
      "type": "DEBIT",
      "amount": 100,
      "currency": "INR",
      "balanceAfter": 900,
      "createdAt": "2026-08-12T10:00:00.234567Z"
    }
  ],
  "nextBeforeId": 42
}
```

`nextBeforeId` is `null` when there is no further page.

---

## `GET /healthz`

Liveness only, no I/O. `200 {"status":"ok"}`.

## `GET /readyz`

Pings the database pool. `200 {"status":"ok"}` or `503 {"status":"unavailable"}`.

## `GET /metrics`

Prometheus exposition format, if the metrics build tag/dependency is present.

---

## Full walkthrough

```bash
# 1. create two user wallets
curl -s -X POST localhost:8080/wallets -d '{"id":"wallet_1","currency":"INR"}'
curl -s -X POST localhost:8080/wallets -d '{"id":"wallet_2","currency":"INR"}'

# 2. fund wallet_1 from the seeded treasury system wallet
curl -s -X POST localhost:8080/transfers -d '{
  "idempotencyKey":"fund-wallet_1",
  "fromWalletId":"treasury_inr",
  "toWalletId":"wallet_1",
  "amount":10000
}'

# 3. transfer wallet_1 -> wallet_2
curl -s -X POST localhost:8080/transfers -d '{
  "idempotencyKey":"abc123",
  "fromWalletId":"wallet_1",
  "toWalletId":"wallet_2",
  "amount":100
}'

# 4. replay the exact same request — identical body, Idempotent-Replay: true, balances unchanged
curl -si -X POST localhost:8080/transfers -d '{
  "idempotencyKey":"abc123",
  "fromWalletId":"wallet_1",
  "toWalletId":"wallet_2",
  "amount":100
}'

# 5. inspect balances and ledger
curl -s localhost:8080/wallets/wallet_1
curl -s localhost:8080/wallets/wallet_2
curl -s 'localhost:8080/wallets/wallet_1/entries'
```

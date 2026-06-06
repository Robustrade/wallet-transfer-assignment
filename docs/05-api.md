# API Contract

## Problem Statement

The handler layer exposes the wallet transfer service over HTTP. Handlers
validate requests, invoke service methods, and map domain errors to appropriate
HTTP status codes. No business logic lives in the handler.

## Endpoints

### `POST /wallets`

Creates a new wallet with a zero balance and a freshly generated UUID v7.

**Request:** No body required.

**Responses:**

| Status | Condition |
|---|---|
| `201 Created` | Wallet created successfully. |
| `500 Internal Server Error` | Unexpected infrastructure failure. |

**Success body (201):**
```json
{
  "id":      "uuid-v7",
  "balance": 0
}
```

---

### `POST /transfers`

Creates a new wallet-to-wallet transfer or returns the cached result for a
previously used idempotency key.

**Request:**
```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "uuid-v7",
  "toWalletId":   "uuid-v7",
  "amount":       10000
}
```
> `amount` is in minor units (e.g. 10000 = 100.00 in a 2-decimal currency).

**Responses:**

| Status | Condition |
|---|---|
| `201 Created` | Transfer executed successfully (first time). |
| `200 OK` | Idempotent replay — same key, same result returned from cache. |
| `400 Bad Request` | Missing `idempotencyKey`, invalid amount (≤ 0), or malformed JSON. |
| `404 Not Found` | `fromWalletId` or `toWalletId` does not exist. |
| `422 Unprocessable Entity` | Insufficient funds in source wallet. |
| `500 Internal Server Error` | Unexpected infrastructure failure. |

**Success body (201 / 200):**
```json
{
  "id":     "uuid-v7",
  "status": "PROCESSED",
  "amount": 10000
}
```
`status` values: `PENDING`, `PROCESSED`, or `FAILED`.

**Error body:**
```json
{ "requestId": "unique request-ID (optional)", "error": "human-readable message" }
```

---

### `GET /transfers/{id}`

Returns the current state of a transfer.

| Status | Condition |
|---|---|
| `200 OK` | Transfer found. |
| `400 Bad Request` | `id` is not a valid UUID. |
| `404 Not Found` | Transfer does not exist. |

**Response body:**
```json
{
  "id":     "uuid-v7",
  "status": "PROCESSED",
  "amount": 10000
}
```

---

### `GET /wallets/{id}`

Returns the current balance of a wallet.

| Status | Condition |
|---|---|
| `200 OK` | Wallet found. |
| `400 Bad Request` | `id` is not a valid UUID. |
| `404 Not Found` | Wallet does not exist. |

**Response body:**
```json
{
  "id":      "uuid-v7",
  "balance": 50000
}
```

---

### `POST /magic`

Deposits a randomly chosen amount between 100 cents and 10 000 cents from the
system *nature* wallet (`c0ffee00-0000-0000-0000-000000000001`) into the
specified destination wallet. The endpoint is idempotent: the same amount is
always returned for a given `idempotencyKey`.

**Request:**
```json
{
  "idempotencyKey": "abc123",
  "toWalletId":     "uuid-v7"
}
```

**Responses:**

| Status | Condition |
|---|---|
| `201 Created` | Deposit executed successfully (first time). |
| `200 OK` | Idempotent replay — same key, same result returned from cache. |
| `400 Bad Request` | Missing or overlong `idempotencyKey`, missing `toWalletId`, or malformed JSON. |
| `404 Not Found` | `toWalletId` does not exist. |
| `500 Internal Server Error` | Unexpected infrastructure failure. |

**Success body (201 / 200):**
```json
{
  "id":     "uuid-v7",
  "status": "PROCESSED",
  "amount": 4231
}
```



The service sets `TransferResponse.FromCache = true` when the response was
served from the idempotency cache. The handler maps this to HTTP 200; fresh
transfers produce HTTP 201. The `FromCache` field is tagged `json:"-"` so it
is never persisted in the cached JSON body.

## Error Mapping

| Domain error | HTTP status |
|---|---|
| `ErrWalletNotFound` / `ErrTransferNotFound` | 404 |
| `ErrInsufficientFunds` | 422 |
| `ErrInvalidAmount` / `ErrSameWallet` | 400 |
| All other errors | 500 |

## Observability

All handler errors are logged with `slog` at ERROR level with:
- `"layer":"handler"`
- `"error": <error message>`
- Entity identifiers (`idempotencyKey`, `transferID`, `walletID`)

## Testing Strategy

Tests in `internal/handler/handler_test.go` use `net/http/httptest` and a
`fakeTransferSvc` test double. No database or Docker is required. Covers all
documented status codes including the 200 vs 201 idempotency distinction.

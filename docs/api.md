# API documentation

## Base URL

The application listens on the configured host and port. By default:

The OpenAPI/Swagger document is available in [swagger.yaml](./swagger.yaml).

```text
http://127.0.0.1:8000
```

## Content type

All JSON request bodies must include:

```http
Content-Type: application/json
```

The server rejects invalid or non-object request bodies with HTTP 400.

## Health check

### GET /health

Returns server health and readiness.

Request:

```http
GET /health
```

Response:

```http
HTTP/1.1 200 OK
Content-Type: application/json

{"status":"ok"}
```

## Create wallet

### POST /wallets

Creates a wallet record.

Request body:

```json
{
  "walletId": "source",
  "initialBalance": 100
}
```

Fields:

- `walletId` — optional string identifier. If omitted, a UUID-based wallet ID is generated.
- `initialBalance` — optional numeric amount. Zero is allowed only for an explicit zero value; the balance is stored as minor units internally.

Response:

```json
{
  "walletId": "source",
  "balance": 100,
  "createdAt": "2026-11-03T14:48:24.012Z"
}
```

Possible status codes:

- `201 Created` on success
- `400 Bad Request` for invalid JSON or invalid amount
- `409 Conflict` if the wallet ID already exists

## Get wallet

### GET /wallets/{walletID}

Fetches a wallet by ID.

Example:

```http
GET /wallets/source
```

Response:

```json
{
  "walletId": "source",
  "balance": 37.5,
  "createdAt": "2026-11-03T14:48:24.012Z"
}
```

Possible status codes:

- `200 OK`
- `404 Not Found` if the wallet does not exist

## Create transfer

### POST /transfers

Moves funds from one wallet to another.

Request body:

```json
{
  "idempotencyKey": "transfer-1",
  "fromWalletId": "source",
  "toWalletId": "destination",
  "amount": 25.5
}
```

Fields:

- `idempotencyKey` — unique business key for this transfer request
- `fromWalletId` — sender wallet ID
- `toWalletId` — receiver wallet ID
- `amount` — numeric value in base units or decimal format, such as `25.5` or `12`

Response on success:

```json
{
  "transferId": "f1c0c511-8a24-4f3a-a7b1-0ecd42c7ac98",
  "idempotencyKey": "transfer-1",
  "fromWalletId": "source",
  "toWalletId": "destination",
  "amount": 25.5,
  "status": "PROCESSED",
  "failureCode": null,
  "createdAt": "2026-11-03T14:48:24.012Z"
}
```

Response on insufficient funds or balance overflow:

```json
{
  "transferId": "a437faa1-3836-4f2a-9d83-e880a1757d9b",
  "idempotencyKey": "transfer-2",
  "fromWalletId": "source",
  "toWalletId": "destination",
  "amount": 50,
  "status": "FAILED",
  "failureCode": "INSUFFICIENT_FUNDS",
  "createdAt": "2026-11-03T14:48:24.012Z"
}
```

Possible status codes:

- `201 Created` when the transfer is processed
- `422 Unprocessable Entity` when the transfer is rejected because it failed validation or insufficient funds
- `400 Bad Request` for malformed JSON or invalid values
- `404 Not Found` when a wallet is missing
- `409 Conflict` when the idempotency key is reused with a different payload

## Idempotency behavior

The transfer endpoint is idempotent by `idempotencyKey`:

- the first request creates a transfer and stores the canonical request hash
- the same key with the same payload returns the original transfer result
- the same key with a different payload fails with `409 Conflict`

This prevents accidental duplicate transfers when a client retries a request after a timeout.

## Error format

Every validation or service error is returned as a JSON object:

```json
{"error": "walletId must be a non-empty string"}
```

## Supported numeric format

The API accepts decimal numeric values such as:

- `1`
- `12.5`
- `25.50`
- `1e2` (scientific notation is accepted by the parser, then normalized to integer minor units)

Amounts are represented as decimal values in JSON and converted to integer minor units internally so the database remains precise and consistent.

# HTTP API

## Create a transfer

`POST /transfers`

The endpoint accepts JSON and requires an idempotency key. The key may be supplied in the `Idempotency-Key` request header or in the JSON `idempotencyKey` field to match the assignment example. If both are supplied, they must match.

Example:

```http
POST /transfers HTTP/1.1
Content-Type: application/json
Idempotency-Key: abc123

{
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

The body form is also supported:

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

The amount is a positive integer in the wallet's smallest currency unit. Floating-point values are rejected by JSON decoding into the integer amount field.

## Success response

A newly processed transfer returns `201 Created`:

```json
{
  "transfer": {
    "id": "transfer-id",
    "fromWalletId": "wallet_1",
    "toWalletId": "wallet_2",
    "amount": 100,
    "status": "PROCESSED"
  }
}
```

A repeated request with the same key and same payload returns the original transfer representation. The handler returns the same success status for a successful replay.

## Error responses

Errors use a stable JSON envelope:

```json
{
  "error": {
    "code": "idempotency_conflict",
    "message": "idempotency key was already used with a different request"
  }
}
```

| HTTP status | Error codes | Meaning |
|---|---|---|
| 400 | `invalid_json`, `invalid_idempotency_key`, `idempotency_key_mismatch`, `invalid_wallet_id`, `same_wallet`, `invalid_amount` | Invalid request |
| 404 | `wallet_not_found` | Source or destination wallet does not exist |
| 409 | `idempotency_conflict` | Key was previously used with a different payload |
| 413 | `request_too_large` | Request body exceeds 1 MiB |
| 415 | `unsupported_media_type` | Content-Type is not application/json |
| 422 | `insufficient_funds`, `currency_mismatch`, `balance_overflow` | Deterministic business failure |
| 500 | `internal_error` | Unexpected internal failure |

Deterministic business failures include the persisted `FAILED` transfer in the response and are replayed with the same status and transfer result. Unexpected infrastructure failures do not expose internal error details.

## Handler boundary

The handler validates transport-level JSON, content type, body size, and idempotency-key consistency. It delegates business validation, idempotency, transaction handling, and transfer execution to `TransferService.ExecuteIdempotent`.

The handler is exposed as `handler.NewTransferHandler(service)`. Application startup and database-driver registration are intentionally left to the application-wiring phase.

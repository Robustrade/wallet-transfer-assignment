# HTTP API

## Create a transfer

`POST /transfers`

The endpoint accepts one JSON object and requires an idempotency key. The key may be supplied in the `Idempotency-Key` request header or in the JSON `idempotencyKey` field. If both are supplied, they must match.

Example using the header:

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

The amount must be a positive integer in the wallet's smallest currency unit. Floating-point values and unknown JSON fields are rejected. The request body is limited to 1 MiB.

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

A repeated request with the same key and same normalized payload returns the persisted HTTP status and complete response envelope from the original request. The response is not rebuilt using current handler mappings, and no additional transfer or ledger entries are created.

## Error response

Errors use this JSON envelope:

```json
{
  "error": {
    "code": "idempotency_conflict",
    "message": "idempotency key was already used with a different request"
  }
}
```

| HTTP status | Error codes | Meaning |
| --- | --- | --- |
| 400 | `invalid_json`, `invalid_idempotency_key`, `idempotency_key_mismatch`, `invalid_wallet_id`, `same_wallet`, `invalid_amount` | Invalid request |
| 404 | `wallet_not_found` | Source or destination wallet does not exist |
| 409 | `idempotency_conflict` | Key was previously used with a different payload |
| 413 | `request_too_large` | Request body exceeds 1 MiB |
| 415 | `unsupported_media_type` | Content-Type is not application/json |
| 422 | `insufficient_funds`, `currency_mismatch`, `balance_overflow` | Deterministic business failure; includes the persisted `FAILED` transfer |
| 500 | `internal_error` | Unexpected internal or persistence failure |

A deterministic business failure is stored with the idempotency record and replayed with the same `422` status and failed transfer. A missing wallet or unexpected infrastructure failure rolls back the transaction and is not stored as a completed idempotency result. Internal error details are not exposed in the HTTP response.

## Handler boundary

The handler handles HTTP method, content type, JSON shape, body size, and idempotency-key consistency. It maps domain/service errors to HTTP status codes and delegates business validation, idempotency, transaction handling, and transfer execution to `TransferService.ExecuteIdempotent`.

The application registers the handler at `/transfers`. See [run-local.md](run-local.md) for startup and [wallet-transfer-design.md](wallet-transfer-design.md) for transaction semantics.

> Historical implementation note. Verification status in this snapshot may be superseded; see the root README and docs/TEST_PLAN.md for the current status.

# Increment 5 — HTTP API and runnable server

## Scope and verification boundary

Increment 4 was **verified by the user on local PostgreSQL**: all concurrency tests, bounded-transaction-retry tests, the full regression suite, and three repeated runs of the concurrency suite passed. This status is based on the user's reported results, not a PostgreSQL run in the artifact-generation environment.

Increment 5 adds only the HTTP layer, runnable executable, and HTTP-level integration tests. **PostgreSQL HTTP tests require a separate local run and are not yet verified.** The current generation environment cannot access PostgreSQL or download `github.com/lib/pq`. HTTP boundary unit tests and domain tests did pass with a temporary compilation-only driver stub; the stub is not included in this project. The real dependency and existing `go.sum` are unchanged.

## Endpoint

`POST /transfers` accepts exactly one JSON object and `Content-Type: application/json` (optional `charset` parameter accepted):

```json
{"idempotencyKey":"order-1001","fromWalletId":"demo-A","toWalletId":"demo-B","amount":100}
```

`amount` is a positive **int64 in minor currency units**, not a floating-point number; this is a one-currency, wallet-to-wallet exercise. The handler rejects malformed JSON, unknown fields, extra top-level objects, and bodies exceeding 8 KiB. Structural validation happens in the domain/service; existence checks happen in PostgreSQL after the idempotency claim. Identifiers containing a NUL byte are now structurally rejected to prevent invalid PostgreSQL text parameters from surfacing as 500 errors.

### Stable outcomes and status codes

| Case | HTTP status | Body | DB effect |
|---|---:|---|---|
| New successful transfer | `201 Created` | `{"transferId":"...","status":"PROCESSED"}` | One committed transfer; debit, credit, two ledger entries |
| Identical key and request replay | `201 Created` | **Same body and HTTP status** as first success | None |
| Insufficient funds | `409 Conflict` | `{"transferId":"...","status":"FAILED","failureReason":"INSUFFICIENT_FUNDS"}` | One terminal FAILED row; no financial entries |
| Replay of the same FAILED request | `409 Conflict` | **Same body/status** as original failure | None |
| Existing key with different operation | `409 Conflict` | `{"error":"idempotency_conflict","message":"..."}` | None |
| New key with missing wallet | `404 Not Found` | `{"error":"wallet_not_found","message":"..."}` | None |
| Structurally invalid request or malformed JSON | `400 Bad Request` | `{"error":"invalid_request" or "invalid_json",...}` | None |
| Wrong Content-Type | `415 Unsupported Media Type` | JSON error | None |
| Body over 8 KiB | `413 Content Too Large` | JSON error | None |
| Wrong method on `/transfers` | `405 Method Not Allowed` | Go ServeMux response | None |
| Transaction failure after bounded retries | `503 Service Unavailable` | `{"error":"outcome_unknown",...}` | Caller must retry same key |
| Unexpected database/server error | `500 Internal Server Error` | Sanitized `outcome_unknown` error | Outcome may be uncertain; retry same key |

For deliberate replay fidelity the API returns only persisted status, transfer ID and failure reason. It does not include current wallet balances, volatile timestamps, or a `replayed` flag. Thus successful first attempts and successful replays both return **201**, by contract; the assignment does not mandate an alternative. No separate idempotency table or response cache is necessary for this fixed response schema. Exact byte-for-byte bodies are checked in HTTP tests.

A `FAILED` transfer is a terminal business outcome, not a technical database failure. Responses with `outcome_unknown` **do not assert that a financial transfer failed**. A client should retry the *same* key after reconnecting.

## Layering and lifecycle

- `internal/handler/transfers.go`: JSON decoding, HTTP mapping and response serialization only; depends on a `Transferer` interface.
- `internal/service/transfer.go`: unchanged business decisions, transaction retry policy and idempotency workflow.
- `internal/repository/postgres.go`: unchanged `READ COMMITTED` transaction, unique-key claim, ascending sorted wallet `FOR NO KEY UPDATE` locks and paired ledger posts.
- `cmd/walletd/main.go`: requires `DATABASE_URL`; `HTTP_ADDR` defaults to `:8080`. Pings the database and checks that all three tables exist before listening; sets connection-pool bounds, appropriate HTTP timeouts, handles Ctrl+C/SIGTERM and calls `http.Server.Shutdown` with a 10-second deadline. It **does not auto-run migrations or seed wallets**.

## HTTP integration tests

`TestPostgresHTTPTransfers` (using `httptest.NewServer`, real `*service.Service`, real repository and isolated PostgreSQL schema) covers:

1. Successful transfer: 201, persisted parameters/status, balances, debit/credit, count, reconciliation.
2. Repeating success with same key: identical status and response bytes, same ID, one transfer and two ledger entries.
3. Changed amount and nonexistent destination with same key: idempotency `409` before wallet-not-found, no additional movement.
4. Insufficient funds: durable FAILED `409` with no ledger entries; reserve-wallet funding via a genuine transfer; failed-key replay still returns original `409` and exact response bytes.
5. Invalid requests: malformed/extra JSON, invalid amounts/IDs, missing wallet, unknown fields, wrong content type, large body, incorrect method; all leave balances and ledger unchanged.
6. Lost HTTP response after commit: discard the response, recreate handler/service objects, replay using same key; original committed ID and financial state are recovered.

Integration tests reuse the existing service-test package fixtures and assertions to minimize changes to the verified PostgreSQL test harness. Unit tests in `internal/handler` cover the HTTP boundary with a stubbed service, but do **not** replace PostgreSQL tests.

## Known scope limitations

- `PENDING` remains an in-transaction intermediate state; only terminal outcomes are committed.
- Early validation errors and a missing wallet for a *new* key are not durably replayed.
- No auth, TLS termination, wallet creation, external funding, health API, GET endpoint or migrations-on-start; the assignment does not require them. The server is intended for local/demo use behind trusted infrastructure.
- The HTTP lost-response test discards the first response **after** a completed request; it does not inject a network disconnect exactly during COMMIT.
- Counterparty credit overflow and privileged out-of-band balance edits are out-of-scope residual risks to discuss during final review.

## Windows verification

From project root in PowerShell, set a **disposable** test DB:

```powershell
$env:WALLET_TEST_DATABASE_URL = 'postgres://postgres:YOUR_PASSWORD@localhost:5432/wallet_transfer_test?sslmode=disable'
go mod download
go mod verify
go test ./internal/handler -count=1 -v
go test ./internal/service -run '^TestPostgresHTTPTransfers$' -count=1 -v -timeout 3m
go test ./... -count=1 -v -timeout 8m
```

The test suite must show `--- PASS: TestPostgresHTTPTransfers` and its six subtests. Reject any `--- SKIP:` and a nonzero Go exit code; see `README.md` for the fail-closed PowerShell verification snippet.

Run server locally using **a separate development DB** with the migration already applied:

```powershell
$env:DATABASE_URL = 'postgres://postgres:YOUR_PASSWORD@localhost:5432/wallet_transfer_dev?sslmode=disable'
$env:HTTP_ADDR = '127.0.0.1:8080'
go run ./cmd/walletd
```

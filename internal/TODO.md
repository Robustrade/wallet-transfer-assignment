# Steps 5 & 6 — Verification & Follow-ups

Verified against `IMPLEMENTATION_GUIDE.md` and `migrations/000001_init_schema.up.sql`.
Build: `go build ./...` ✓ · Vet: `go vet ./internal/domain/... ./internal/repository/...` ✓

---

## Step 5 — Domain layer (`internal/domain/`)

### Done

| Requirement | Status | Notes |
|-------------|--------|-------|
| `wallet.go` — `Wallet` (ID, Balance, timestamps) | ✓ | |
| `transfer.go` — `Transfer`, `TransferRequest` | ✓ | |
| `transfer.go` — response DTO | ✓ | Named `TransferResponse` (guide uses `TransferResponse`) |
| `ledger.go` — `LedgerEntry`, `DEBIT` / `CREDIT` | ✓ | |
| `status.go` — `PENDING`, terminal states | ✓ | See alignment gaps below |
| `errors.go` — sentinel errors | ✓ | |
| `TransferRequest.Validate()` | ✓ | Empty idempotency key, empty wallet IDs, same wallet, `amount <= 0` |
| Pure domain (no HTTP / SQL / DB imports) | ✓ | stdlib only: `errors`, `strings`, `time` |

### Gaps / defer to later

- [ ] **Rename `TransferResponse` → `TransferResponse`** to match guide (optional).
- [ ] **Document API contract:** `amount` in JSON is whole rupees (INR), e.g. `"amount": 100` = Rs 100.
- [ ] **`TransferResponse.Error`:** won't JSON-serialize for idempotency replay — handle in service/handler.
- [ ] **Unit tests:** `Validate()` table tests (deferred).

---

## Step 6 — Repository layer (`internal/repository/`)

```text
internal/repository/
├── repository.go          # Repository struct, New
├── transfer_repository.go   # TransferRepository interface + methods
├── wallet_repository.go     # WalletRepository interface + methods
├── transaction.go           # WithTx
└── errors.go
```

`*Repository` implements both `TransferRepository` and `WalletRepository`.

### Methods on `Repository`

| Method | Purpose |
|--------|---------|
| `FindByIdempotencyKey` | Load transfer by idempotency key |
| `FindIdempotencyRecord` | Replay stored response |
| `CreateTransfer` | Insert pending transfer |
| `UpdateTransferStatus` | PENDING → PROCESSED / FAILED |
| `InsertLedgerEntries` | DEBIT + CREDIT rows |
| `SaveIdempotencyRecord` | Store replay payload |
| `GetWalletForUpdate` | `SELECT … FOR UPDATE` |
| `UpdateBalance` | `balance + delta` |

### Gaps / defer to later

- [ ] **Integration tests** against docker Postgres.
- [ ] **`CreateTransfer` timestamps:** service should set `time.Now()` if not set.

---

## Ready for Step 7?

**Yes.** Wire `repository.New(pool)` into the service. Use `repo.WithTx(ctx, fn)` for the transfer flow.

1. All amounts are whole rupees (`int64`) — wallet balance, transfer amount, ledger entries.
2. Use `domain.PROCESSED` for successful transfers.
3. Build idempotency JSON explicitly (don't rely on `TransferResponse.Error`).

---

## Suggested next steps (Step 7+)

1. Implement `internal/service/transfer_service.go` (orchestration flow from guide).
2. Align domain money/status types (optional but recommended before heavy testing).
3. Add repository integration tests + domain `Validate()` unit tests.

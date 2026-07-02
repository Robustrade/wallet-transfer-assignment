# Wallet Transfer — Test Plan

Seeded test wallets: `wallet-alice` (100000), `wallet-bob` (50000).
Convention: sender = money leaves this wallet, receiver = money lands here.

## 1. Request validation (no DB)
- Reject empty idempotency key, empty from/to wallet ID, or sender == receiver.
- Reject zero or negative amount.

## 2. Happy path
- Transfer 100 from alice to bob → 201, status `PROCESSED`.
- Sender balance -100, receiver balance +100.
- Exactly one transfer row created; response includes the transfer ID.

## 3. Ledger
- Every transfer writes exactly 2 ledger entries: a DEBIT on the sender and a CREDIT on the receiver, both for the same amount.
- Retrying with the same idempotency key doesn't add more ledger rows.

## 4. Idempotency
- Same key sent twice → same response and transfer ID, balances only move once.
- First call persists a row in `idempotency_records`.
- A failed transfer (e.g. insufficient funds) is idempotent too — retrying doesn't retry the side effects.
- Two concurrent requests with the same key → only one transfer actually happens; both callers get the same result.

## 5. States & failure modes
- Success → `PROCESSED`. Insufficient funds → `FAILED`, no balance change, no ledger rows.
- Status codes: insufficient funds → 409, unknown wallet → 404, malformed/empty JSON → 400.

## 6. Concurrency
- Sender has 150; two parallel 100-transfers to different receivers (different keys) → only one succeeds, final balance is 50 (never negative).
- Sender has 1000; ten parallel 100-transfers → all succeed, final balance 0.
- alice→bob and bob→alice fired simultaneously → both succeed (assuming sufficient funds), final balances correct.

## 7. HTTP handler
- Valid POST → 201 with JSON body. Validation failure → 400. Unknown wallet → 404. Insufficient funds → 409.

## 8. DB constraints (optional/nice-to-have)
- Unique idempotency key, no same-wallet transfers, no zero-amount transfers, one DEBIT per transfer enforced at the DB level.

---

**Order:** validation → happy path → ledger → idempotency → states/failures → concurrency → handler.

**Setup notes:** use the seeded wallets or create ones with known balances per test. Postgres via docker-compose, DB reset between tests, fresh idempotency key per test unless you're specifically testing duplicates.

**Out of scope for now:** balance API, history API, metrics — skip until those exist.
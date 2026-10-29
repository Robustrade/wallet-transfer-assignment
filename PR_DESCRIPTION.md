## Summary

Wallet transfer service (Java 17, Spring Boot 3, PostgreSQL, Flyway) with idempotent `POST /transfers`,
a double-entry ledger, stored wallet balances, a `PENDING -> PROCESSED | FAILED` state machine and
row-lock based concurrency control. Layers: `handler` -> `service` -> `repository` + `domain`.
Design note: `docs/DESIGN.md`.

## AI disclosure

1. **Tool used:** Claude (Cowork mode in the Claude desktop app).
2. **How I use it:** I give the tool the requirements and my constraints (stack, database, scope), let it produce a first draft of the design, code, tests and docs, and then review that draft critically: I check the transaction and locking logic, the idempotency flow and the tests, and I change what I disagree with. I do not accept output I cannot explain.
3. **In this submission:** `docs/design-decisions.md`.

## Schema Design

* `wallets` (balance `CHECK >= 0`), `transfers` (UNIQUE `idempotency_key`, amount > 0, status CHECK,
  `from <> to`), `ledger_entries` (FKs, `UNIQUE(transfer_id, entry_type)` => exactly one DEBIT and one CREDIT).
* Indexes for key lookup, per-wallet history and ledger by wallet.
* The `transfers` row doubles as the idempotency record (no separate table); see DESIGN.md.

## Idempotency Strategy

Insert a `PENDING` transfer with `ON CONFLICT (idempotency_key) DO NOTHING`. If nothing was inserted
the stored transfer is returned (after checking a request hash, so key reuse with a different payload
is a 422). Concurrent duplicates serialize on the unique index. Failed transfers are replayed as failed.

## Concurrency Strategy

One transaction per transfer; both wallets locked with `SELECT ... FOR UPDATE ORDER BY id` (no
deadlocks, no read-then-write race), balance checked after locking, plus guarded debit, CHECK
constraint and compare-and-set status updates as defence in depth.

## How to Run

```bash
# start a local PostgreSQL 16 with database/user/password 'wallet' (see src/main/resources/application.yml; override with DB_URL, DB_USER, DB_PASSWORD)
mvn spring-boot:run
curl -X POST localhost:8080/wallets -H 'Content-Type: application/json' -d '{"id":"wallet_1","openingBalance":1000}'
curl -X POST localhost:8080/wallets -H 'Content-Type: application/json' -d '{"id":"wallet_2","openingBalance":0}'
curl -X POST localhost:8080/transfers -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
```

## How to Test

```bash
mvn verify   # needs Docker running, because the tests start PostgreSQL via Testcontainers
```

## Tradeoffs / Assumptions

* Amounts are integer minor units; single currency; `idempotencyKey` required.
* Pessimistic locking over optimistic/serializable (hot-wallet friendly, no retry loops).
* Opening balances are seeded, not ledger entries.
* No auth, no key expiry, logging only (no metrics).
* CI: workflow is Go-oriented; set `LINT_CMD=mvn -B -q verify -DskipTests`, `FORMAT_CHECK_CMD=true`,
  `TEST_CMD=mvn -B verify` and add a JDK 17 setup step.

## Checklist

- [ ] Tests pass (run `mvn verify` locally first)
- [ ] Lint passes
- [ ] Format check passes
- [x] README or notes updated
- [x] PR description explains schema, idempotency, and concurrency

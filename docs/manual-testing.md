# Manual testing with curl

A step-by-step walkthrough of every API behavior, with the exact response to expect. It works
the same on SQLite and PostgreSQL.

> **Verified 2026-09-28:** every step below was run exactly as written against a fresh database, on
> both SQLite and PostgreSQL 14.20. Both gave identical statuses, messages and final balances.

The same scenarios, as clickable requests: [`requests.http`](../requests.http) (VS Code REST Client /
JetBrains HTTP Client). Automated versions: `make demo` (walkthrough) and `make verify-pg` (Postgres end to end).

## 0. Start the server on a fresh database

The expected balances in step 10 assume a **fresh database** (demo seed only).

**SQLite**

```sh
rm -f wallet.db wallet.db-wal wallet.db-shm     # start fresh
make run
```

**PostgreSQL** (see [postgres.md](postgres.md) to set one up)

```sh
psql 'postgres://wallet:wallet@localhost:5432/postgres' -c 'drop database if exists wallet' -c 'create database wallet'
DB_DRIVER=postgres DB_DSN='postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable' make run
```

The first log line should include `"db_driver":"sqlite"` or `"db_driver":"postgres"`. Keep the
server running. It prints one JSON log line per request, so you can watch each step from the server
side.

The demo seed:

| Wallet | Balance | Currency |
|---|---|---|
| `wallet_1` | 100000 | SGD |
| `wallet_2` | 50000 | SGD |
| `wallet_3` | 0 | SGD |
| `wallet_usd` | 10000 | USD |

Amounts are integer minor units (100000 = 1000.00).

Run the steps below in a second terminal. `-i` prints the status line and headers.

## 1. Health

```sh
curl -i localhost:8080/healthz      # 200 {"status":"ok"}     process is up (never touches the DB)
curl -i localhost:8080/readyz       # 200 {"status":"ready"}  database reachable (503 if not)
```

## 2. Transfers

**① Normal transfer: 201 PROCESSED**

```sh
curl -i -X POST localhost:8080/transfers -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"my-test-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":500}'
```

```
HTTP/1.1 201 Created
{"id":"01a0e77f-…","idempotencyKey":"my-test-1","fromWalletId":"wallet_1","toWalletId":"wallet_2",
 "amount":500,"status":"PROCESSED","failureReason":null,"createdAt":"…"}
```

Note the `id`.

**② The exact same request again: 201 replay, no money moves**

```sh
curl -i -X POST localhost:8080/transfers -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"my-test-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":500}'
```

```
HTTP/1.1 201 Created
Idempotent-Replayed: true
{"id":"01a0e77f-…" (same id as ①), … same body as ① …}
```

**③ Same key, different amount: 409**

```sh
curl -i -X POST localhost:8080/transfers -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"my-test-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":999}'
```

```
HTTP/1.1 409 Conflict
{"error":{"code":"IDEMPOTENCY_KEY_REUSED","message":"idempotencyKey was already used with a different request","requestId":"…"}}
```

**④ Insufficient funds: 422 FAILED** (`wallet_3` holds 0)

```sh
curl -i -X POST localhost:8080/transfers -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"my-test-2","fromWalletId":"wallet_3","toWalletId":"wallet_1","amount":100}'
```

```
HTTP/1.1 422 Unprocessable Entity
{"id":"…","idempotencyKey":"my-test-2",…,"status":"FAILED","failureReason":"INSUFFICIENT_FUNDS",…}
```

The FAILED transfer is **stored**. Repeating this request replays FAILED, even if `wallet_3` is
topped up later.

**⑤ Currency mismatch: 422 FAILED**

```sh
curl -i -X POST localhost:8080/transfers -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"my-test-3","fromWalletId":"wallet_1","toWalletId":"wallet_usd","amount":100}'
```

```
HTTP/1.1 422 Unprocessable Entity
{…,"status":"FAILED","failureReason":"CURRENCY_MISMATCH",…}
```

**⑥ Unknown wallet: 404, nothing stored**

```sh
curl -i -X POST localhost:8080/transfers -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"my-test-4","fromWalletId":"wallet_1","toWalletId":"nobody","amount":100}'
```

```
HTTP/1.1 404 Not Found
{"error":{"code":"WALLET_NOT_FOUND","message":"wallet not found","requestId":"…"}}
```

Nothing is stored, so `my-test-4` stays usable (for example, once the wallet exists).

## 3. Validation: 400 VALIDATION_ERROR, nothing stored

| Request | `message` |
|---|---|
| `"amount":10.5` | `amount: must be an integer number of minor units` |
| `"amount":"100"` | `amount: must be an integer number of minor units` |
| `"toWalletId":"wallet_1"` (same as from) | `toWalletId: must differ from fromWalletId` |
| extra field `"note":"x"` | `body: contains unknown field "note"` |
| `Content-Type: text/plain` | `Content-Type: must be application/json` |

```sh
curl -i -X POST localhost:8080/transfers -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"my-test-5","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":10.5}'

curl -i -X POST localhost:8080/transfers -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"my-test-5","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":"100"}'

curl -i -X POST localhost:8080/transfers -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"my-test-5","fromWalletId":"wallet_1","toWalletId":"wallet_1","amount":1}'

curl -i -X POST localhost:8080/transfers -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"my-test-5","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":1,"note":"x"}'

curl -i -X POST localhost:8080/transfers -H 'Content-Type: text/plain' \
  -d '{"idempotencyKey":"my-test-5","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":1}'
```

Other cases rejected the same way:
- missing, empty, too long, or whitespace-containing `idempotencyKey`;
- `amount` equal to 0, negative, `null`, `1e2`, or above 100000000;
- malformed JSON, an empty body, or a JSON array.

## 4. Request id

```sh
curl -i localhost:8080/healthz -H 'X-Request-ID: my-trace-123'
# X-Request-Id: my-trace-123   (echoed; without the header a UUID is generated)
```

Every error body carries the same id in `error.requestId`, and so does the server's log line.

## 5. Concurrency: 10 identical requests at once, one transfer

```sh
for i in $(seq 1 10); do
  curl -s -o /dev/null -D - -X POST localhost:8080/transfers -H 'Content-Type: application/json' \
    -d '{"idempotencyKey":"my-burst-1","fromWalletId":"wallet_2","toWalletId":"wallet_3","amount":50}' \
    | grep -i 'replayed' &
done; wait
```

Expect **9** lines of `Idempotent-Replayed: true`. One request executed, and the other nine waited
on the idempotency key and then replayed it.

## 6. Final balances and history

```sh
curl localhost:8080/wallets/wallet_1
curl localhost:8080/wallets/wallet_2
curl localhost:8080/wallets/wallet_3
curl 'localhost:8080/wallets/wallet_1/transfers?limit=5'
```

| Wallet | Expected | Why |
|---|---|---|
| `wallet_1` | **99500** | 100000 − 500 (①) |
| `wallet_2` | **50450** | 50000 + 500 (①) − 50 (burst) |
| `wallet_3` | **50** | 0 + 50 (burst); ④ FAILED moved nothing |
| `wallet_usd` | **10000** | ⑤ FAILED moved nothing |

The history for `wallet_1` lists **3** transfers, newest first: ⑤, ④ and ①. A FAILED transfer is
still part of the wallet's history.

Only ① and the burst moved money. Everything else was a replay, a rejection, or a FAILED transfer
that is stored with no ledger lines.

## 7. Check the database

**PostgreSQL**

```sh
psql 'postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable' -c \
  "select idempotency_key, status, failure_reason, amount from transfers where idempotency_key like 'my-%' order by created_at"
```

```
 idempotency_key |  status   |   failure_reason   | amount
-----------------+-----------+--------------------+--------
 my-test-1       | PROCESSED |                    |    500
 my-test-2       | FAILED    | INSUFFICIENT_FUNDS |    100
 my-test-3       | FAILED    | CURRENCY_MISMATCH  |    100
 my-burst-1      | PROCESSED |                    |     50
```

There is no row for `my-test-4` (the 404) or `my-test-5` (validation), and exactly one row for
the 10-request burst. More queries (ledger lines, money conservation, constraint checks) are in
[postgres.md](postgres.md#manual-checks).

**SQLite**

```sh
sqlite3 wallet.db "select idempotency_key, status, failure_reason, amount from transfers where idempotency_key like 'my-%' order by created_at"
```

This needs the `sqlite3` CLI. The server itself needs nothing extra.

## Re-running

Idempotency keys are permanent. That is the point of them, so a second run replays ① instead of
moving money again, and the balances from step 6 no longer apply. To start over, either:
- use new keys (`my-test-1` → `my-test-11`, …), or
- reset the database as in step 0.

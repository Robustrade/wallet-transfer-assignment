# Running the Wallet Transfer Service

This is the operational guide: how to start the service, how to run it
against Postgres, how to exercise the API, and how to run the test suite.
For architecture and design rationale, see [README.md](README.md).

## Prerequisites

- Go 1.27+ (`go version`)
- Docker + Docker Compose, only if you want to run against Postgres
  instead of the in-memory store

## 1. Quick start — no database required

The service falls back to an in-memory store when `DATABASE_URL` is unset,
so you can run it with zero setup:

```bash
go run ./main
```

You should see:

```
DATABASE_URL not set, using in-memory store (dev/demo mode)
wallet transfer service listening on :8080
```

The in-memory store is per-process and non-persistent — restarting the
process resets all wallets, transfers, and ledger entries. It's fine for
local exploration and for the test suite, not for anything you need to
survive a restart.

## 2. Running against Postgres

```bash
docker compose up -d
```

This starts Postgres 16 on host port **5433** (mapped from the
container's 5432, so it won't collide with a local Postgres on 5433/5432)
and automatically applies [migrations/0001_init.sql](migrations/0001_init.sql)
on first boot via Postgres's `docker-entrypoint-initdb.d` mechanism. That
only runs once against a fresh data volume — see
[Resetting the database](#resetting-the-database) if you change the
migration and need it to re-run.

Wait for the container to report healthy, then point the app at it:

```bash
# bash
export DATABASE_URL="postgres://wallet:wallet@localhost:5433/wallet_transfer?sslmode=disable"
go run ./main
```

```powershell
# PowerShell
$env:DATABASE_URL = "postgres://wallet:wallet@localhost:5433/wallet_transfer?sslmode=disable"
go run ./main
```

You should see `connected to Postgres` instead of the in-memory message.

To stop the database:

```bash
docker compose down          # stop, keep data
docker compose down -v       # stop and wipe the data volume
```

### Resetting the database

`docker-entrypoint-initdb.d` scripts only run against an empty data
volume. If you've edited `migrations/0001_init.sql` (or want a clean
slate), recreate the volume:

```bash
docker compose down -v
docker compose up -d
```

## 3. Configuration

| Env var        | Default             | Purpose                                                              |
|----------------|----------------------|-----------------------------------------------------------------------|
| `DATABASE_URL` | *(unset)*            | Postgres DSN. Unset → in-memory store. Set → connects to Postgres.    |
| `HTTP_ADDR`    | `:8080`              | Listen address for the HTTP server.                                  |

Example running on a different port:

```bash
HTTP_ADDR=:9090 go run ./main
```

The server shuts down gracefully on `SIGINT`/`SIGTERM` (Ctrl+C), draining
in-flight requests for up to 10 seconds before exiting.

## 4. Using the API

All amounts and balances are integers in the smallest currency unit
(e.g. cents) — never floats.

### Health check

```bash
curl http://localhost:8080/healthz
```

### Create a wallet

```bash
curl -X POST http://localhost:8080/wallets \
  -H "Content-Type: application/json" \
  -d '{"id": "wallet_1", "initialBalance": 1000}'

curl -X POST http://localhost:8080/wallets \
  -H "Content-Type: application/json" \
  -d '{"id": "wallet_2", "initialBalance": 0}'
```

### Check a balance

```bash
curl http://localhost:8080/wallets/wallet_1
```

### Create a transfer

```bash
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
        "idempotencyKey": "abc123",
        "fromWalletId": "wallet_1",
        "toWalletId": "wallet_2",
        "amount": 100
      }'
```

### See idempotency in action

Re-run the exact same `curl` command above (same `idempotencyKey`, same
body). It returns the original result instead of debiting again — check
`wallet_1`'s balance before and after to confirm it doesn't move on the
replay.

Re-running it with the **same key but a different body** (e.g. a
different `amount`) returns `422 Unprocessable Entity` instead of
silently applying either version — the key is bound to the exact request
that first claimed it.

### Fetch a transfer (with its ledger entries)

```bash
curl http://localhost:8080/transfers/<transferId>
```

`<transferId>` comes from the `transferId` field in the create-transfer
response.

### Using Postman instead

A Postman environment and globals file are checked in under
[postman/environments/](postman/environments/) and
[postman/globals/](postman/globals/) — import them into Postman and use
the requests above as a reference for building out the collection, or
point the imported environment's base URL at `http://localhost:8080`.

## 5. Running the tests

```bash
go test ./...
```

Verbose, per-test output:

```bash
go test ./... -v
```

Run only the service-layer tests (where all business logic and
concurrency tests live):

```bash
go test ./internal/service/... -v
```

Repeat runs to shake out flaky concurrency behavior (useful since Go's
scheduler varies goroutine interleaving run to run):

```bash
go test ./internal/service/... -count=20
```

### Race detector

```bash
go test ./... -race
```

`-race` requires cgo, which needs a C compiler (`gcc`/`clang`) on
`PATH`. If you see `-race requires cgo; enable cgo by setting
CGO_ENABLED=1` and don't have a compiler installed, use the repeated
`-count=N` run above as a fallback — it won't catch every data race a
race-detector run would, but it does catch logic bugs that only surface
under real goroutine interleaving (wrong balances, lost updates,
duplicate transfers).

### What the tests cover

`internal/service/*_test.go` covers, behaviorally (against the public
service API, not internals):

- **transfer execution** — success path, balance updates, boundary cases
  (exact-balance transfer, one-unit-over-balance)
- **idempotency** — replay of successful *and* failed transfers, key
  reuse with a conflicting payload, concurrent races on the same key
- **ledger correctness** — debit/credit pairing, per-transfer and
  global (multi-transfer) double-entry balance, entries attributed to
  the correct wallet
- **failure scenarios** — insufficient funds, missing wallet, same
  source/destination wallet, invalid/zero/negative amount, empty
  idempotency key
- **concurrency safety** — many goroutines racing to overdraw one
  wallet, opposite-direction transfers between the same wallet pair
  (the classic lock-ordering deadlock shape), and a conservation-of-funds
  check across many wallets under concurrent load

## 6. Troubleshooting

**`bind: address already in use` on startup** — something else is on
`:8080`. Set `HTTP_ADDR` to a free port, or stop the other process.

**Postgres connection refused** — the container may still be starting;
check `docker compose ps` for `healthy` status, or `docker compose logs
postgres`.

**Migration doesn't seem applied** — see
[Resetting the database](#resetting-the-database); init scripts only run
once, against an empty volume.

**Port 5433 already in use** — another Postgres (or a previous run of
this compose file) is using it. Change the host-side port mapping in
[docker-compose.yml](docker-compose.yml) (`"5433:5432"`) and update
`DATABASE_URL` to match.

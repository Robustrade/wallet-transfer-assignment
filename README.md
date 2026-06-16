# Wallet Transfer Service

A Spring Boot service for moving funds between wallets. Two design properties drive most of the code:

- **Exactly-once transfers under retries.** Every `POST /transfers` is keyed by `(X-Client-Id, Idempotency-Key)`; replays return the cached response, never re-execute.
- **Auditable balances.** Every transfer writes a paired double-entry into `ledger_entries`. The ledger is the source of truth; `wallets.balance` is a maintained projection. Both ledger rows and wallets are append-only at the database level (DB triggers reject `UPDATE`/`DELETE`).

For the problem framing this implementation answers, see [`ASSIGNMENT.md`](./ASSIGNMENT.md). For the design rationale — *why* the system is shaped this way — see [`docs/DESIGN.md`](./docs/DESIGN.md) (read top-to-bottom) and [`docs/DESIGN_DEEP_DIVE.md`](./docs/DESIGN_DEEP_DIVE.md) (reference: race walkthroughs, alternatives rejected, mechanics). For the client-side retry contract (status codes, when to retry, when not to), see [`docs/RETRY_CONTRACT.md`](./docs/RETRY_CONTRACT.md).

> [!WARNING]
> **HMAC signature validation is OFF by default.**
>
> `security.signature.enabled=false` ships in `application.yml` so the service can be exercised without provisioning a shared secret. In this mode any caller that knows a valid `Idempotency-Key` and `X-Client-Id` can move money — there is no proof of caller identity. **Do not run this configuration in production.** Enable it (`SIGNATURE_ENABLED=true`, `SIGNATURE_SECRET=<32+ random bytes>`) before opening the service to traffic; alert on the `WARN` line emitted at startup when the flag is off.

## Stack

Java 21 · Spring Boot 3.3 · jOOQ (codegen from migrations) · PostgreSQL 16 · Flyway · Micrometer/Prometheus · Testcontainers.

## API

All endpoints require `X-Client-Id`. `POST /transfers` additionally requires `Idempotency-Key`. When HMAC is enabled, `X-Signature` and `X-Timestamp` are also required.

| Endpoint                                | Purpose                                       |
| --------------------------------------- | --------------------------------------------- |
| `POST /transfers`                       | Create a transfer (idempotent on the key).    |
| `GET /wallets/{id}/balance`             | Current balance of a wallet.                  |
| `GET /wallets/{id}/transfers?limit=N`   | Most-recent transfers for a wallet.           |
| `GET /actuator/health`                  | Liveness / readiness probe.                   |
| `GET /actuator/prometheus`              | Metrics scrape endpoint.                      |

`POST /transfers` returns `201` on first success, `200` on idempotent replay, `409` on key reuse with a different payload, `422` on insufficient balance, `503` (with `Retry-After`) when bounded retries exhaust, and `504` when DB timeouts trip. Full status table in [`docs/RETRY_CONTRACT.md`](./docs/RETRY_CONTRACT.md).

## Local setup

### Option A — Docker Compose (recommended)

Brings up Postgres and the app together. Requires Docker.

```bash
cp .env.example .env        # edit DB_PASS for anything beyond a quick local run
docker compose up --build
```

The app listens on `http://localhost:8080`. Postgres is exposed on `5432`. Data is persisted to a named volume (`pgdata`) so restarts keep state.

### Option B — Maven against a local Postgres

```bash
# 1. Start a Postgres 16 with credentials matching application.yml defaults
docker run -d --name wallet-pg -p 5432:5432 \
  -e POSTGRES_DB=wallet_db -e POSTGRES_USER=wallet_user -e POSTGRES_PASSWORD=wallet_pass \
  postgres:16-alpine

# 2. Run the app — Flyway applies migrations, jOOQ codegen runs from src/main/resources/db/migration
./mvnw spring-boot:run
```

`DB_HOST`, `DB_PORT`, `DB_NAME`, `DB_USER`, `DB_PASS` override the defaults if you point at a different Postgres.

## Tests

All non-trivial paths are covered by integration tests using Testcontainers, which spins up a real Postgres per test class.

```bash
./mvnw test                      # local — needs Docker daemon reachable
docker compose --profile test up --build test   # containerised — mounts the Docker socket (dev/CI only)
```

Unit tests do not require Docker; integration tests do.

## Configuration

All knobs are environment-overridable. Defaults are tuned for local development; review them before promoting to a shared environment.

| Variable                                  | Default     | What it does                                                              |
| ----------------------------------------- | ----------- | ------------------------------------------------------------------------- |
| `DB_HOST` / `DB_PORT` / `DB_NAME`         | `localhost` / `5432` / `wallet_db` | Postgres connection target.                            |
| `DB_USER` / `DB_PASS`                     | `wallet_user` / `wallet_pass`      | Postgres credentials. **Replace `DB_PASS` everywhere except local.** |
| `SIGNATURE_ENABLED`                       | `false`     | Turn on HMAC request authentication. **Set `true` in prod.**              |
| `SIGNATURE_SECRET`                        | _empty_     | 32+ bytes of random secret material. Must be supplied via secret store.   |
| `SIGNATURE_TTL_MINUTES`                   | `5`         | `X-Timestamp` freshness window. Wider windows enlarge the replay surface. |
| `TRANSFER_RETRY_MAX_ATTEMPTS`             | `3`         | Cap on conflict-then-rollback retries before surfacing `503`.             |
| `TRANSFER_RETRY_BASE_BACKOFF_MILLIS`      | `10`        | Base back-off between attempts.                                           |
| `TRANSFER_RETRY_JITTER_MILLIS`            | `10`        | Uniform jitter on top of the base back-off.                               |
| `TRANSFER_RETRY_AFTER_SECONDS`            | `1`         | `Retry-After` header on the `503`.                                        |
| `TRANSFER_TRANSACTION_TIMEOUT_SECONDS`    | `10`        | Per-attempt transaction budget. Keep ≤ `statement_timeout`.               |
| `IDEMPOTENCY_CLEANUP_RETENTION_HOURS`     | `72`        | How long to keep `COMPLETED`/`FAILED` records before eviction.            |
| `IDEMPOTENCY_CLEANUP_BATCH_SIZE`          | `1000`      | Rows deleted per `DELETE` statement during the sweep.                     |
| `IDEMPOTENCY_CLEANUP_MAX_BATCHES_PER_TICK`| `50`        | Cap on batches per scheduler tick.                                        |
| `IDEMPOTENCY_CLEANUP_CRON`                | `0 0 * * * *` | Sweep schedule (top of every hour by default).                          |
| `WALLET_TRANSFER_HISTORY_MAX_LIMIT`       | `500`       | Hard ceiling for `GET /wallets/{id}/transfers?limit=`.                    |

The Postgres connection is initialised with `lock_timeout=8s` and `statement_timeout=10s` (`application.yml` → `connection-init-sql`). These are enforced server-side by Postgres — surfaced to clients as `504` — and bound the request budget even when `@Transactional` cannot.

## Deployment

The application is a single immutable JAR (`target/app.jar`) built from `Dockerfile`. The image runs as a non-root user (`uid 10001`) and exposes port `8080`.

### Cloud / staging

- Wire the database via your platform's managed Postgres (RDS, Cloud SQL, Neon, etc.). Provide `DB_*` env vars; nothing else changes.
- Inject `SIGNATURE_SECRET` from the platform's secret store (AWS Secrets Manager, GCP Secret Manager, Vault). **Never** bake it into the image or commit it.
- Point the platform health probes at `/actuator/health`. The container has a built-in `HEALTHCHECK` for orchestrators that honour it.
- Scrape `/actuator/prometheus` from your metrics stack. The `transfers_execute_duration_seconds` histogram has an `outcome` tag that distinguishes `success`, `replay`, `insufficient_balance`, `wallet_not_found`, `transient_conflict`, `hash_conflict`, `in_progress`, `unknown_client`, and `error` — alerting on the `error` and `transient_conflict` buckets is the most useful starting point.

### Production

In addition to the above:

- **Set `SIGNATURE_ENABLED=true`.** Confirm in the startup log that the flag took effect; alert on the `WARN` line emitted when it is `false`.
- **Provision sufficient DB connections.** Hikari is sized to `maximum-pool-size: 20` per app instance — multiply by replica count when sizing the Postgres `max_connections` budget. Add `+5` headroom per instance for Flyway/cleanup/diagnostics.
- **Tune `TRANSFER_RETRY_MAX_ATTEMPTS` for your contention profile.** The default (`3`) is conservative; raise it temporarily during a hot-shard incident to convert `503`s into successful retries, but do not leave it elevated permanently — it widens the latency tail.
- **Do not raise `IDEMPOTENCY_CLEANUP_RETENTION_HOURS` without sizing the table.** The retention window doubles as the maximum useful "I lost the response, retry me" window for the client. A 72h window is enough for human-in-the-loop retries and short enough that the table stays bounded under reasonable traffic.
- **Run at least two replicas behind a load balancer.** The cleanup sweep uses a Postgres advisory lock for leader election, so multiple instances are safe — only one runs the sweep per tick.
- **Take regular Postgres backups.** The ledger is the audit record; `wallets.balance` can be reconstructed from it, but only if you still have it.

## Things to keep in mind

- **One idempotency key per logical transfer attempt.** Reusing a key with a different payload is a `409` (intentional — protects against client bugs that quietly substitute amount or wallets). If a caller is genuinely retrying the same logical attempt, it must send the same body byte-for-byte.
- **`X-Client-Id` is not authentication.** It namespaces the idempotency key and identifies the calling system in logs. With HMAC enabled it is the lookup key for the shared secret; without HMAC it is unauthenticated and trivially forgeable. Production deployments must enable HMAC.
- **Wallet ownership is not enforced at the API layer.** Any authenticated client can move funds between any pair of wallets. If your domain requires per-wallet ACLs, add them at the controller layer — this service does not.
- **The ledger and wallets are append-only at the DB level.** DB triggers reject `UPDATE` and `DELETE` on `ledger_entries`, and `DELETE` on `wallets`. This is deliberate. Reversing a transfer requires posting a compensating transfer, not editing rows. If you need a "soft delete" for wallets, that is a schema change — see `V3__enforce_wallet_immutability.sql`.
- **Idempotency records are evicted after `retention-hours`.** Replays after the retention window will re-execute as fresh requests. Tune retention with your retry SLA.
- **`503` with `Retry-After` is not the same as `409`.** `503` means "system is contended, try again in N seconds with the *same* payload"; `409` means "you sent a different payload for this key, fix your request." Client retry logic should honour the distinction (see `docs/RETRY_CONTRACT.md`).
- **Database timeouts surface as `504`.** A `504` from this service usually means a Postgres-side timeout fired — either `statement_timeout` (SQLState `57014`) or `lock_timeout` (SQLState `55P03`). Investigate query plans and lock waits before raising the timeout.

## Project layout

```
src/main/java/com/wallet/   Spring application code (controller / service / repository / domain)
src/main/resources/
  application.yml           All configuration knobs
  db/migration/             Flyway migrations — V1 schema, V2 cleanup index, V3 immutability triggers
src/test/java/com/wallet/   Unit + integration tests (Testcontainers)
docs/DESIGN.md              Design rationale (the why; read top-to-bottom)
docs/DESIGN_DEEP_DIVE.md    Mechanics, race walkthroughs, alternatives rejected
docs/RETRY_CONTRACT.md      Client retry semantics
ASSIGNMENT.md               Original problem statement
```

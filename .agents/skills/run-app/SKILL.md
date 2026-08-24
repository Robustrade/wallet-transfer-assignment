---
name: run-app
description: Launch the wallet transfer service locally or in Docker, apply migrations (with or without demo seed data), verify it's actually serving requests, and drive the concurrency demo script. Use whenever asked to run, start, demo, or manually verify this app.
---

# Run the Wallet Transfer Service

Two ways to run it — pick based on what's being verified.

## Option A: full containerized stack (the interview demo)

```
just demo
```

This runs, in order: `just up` (starts the docker-compose Postgres, waits for `pg_isready`),
`SEED_DEMO_DATA=true just migrate` (schema + the four demo wallets from
`db/seed/V9001__seed_demo_wallets.sql`, ADR-0010), then builds and starts the `app` container
from `Dockerfile`. The app does **not** run migrations itself (ADR-0004 — migrations are a
deploy-time concern via Flyway, never part of the service's runtime path), which is why `demo`
sequences migrate-then-app rather than letting `docker compose up` race them.

Verify it's actually up:

```
curl -sS http://localhost:8000/docs -o /dev/null -w "%{http_code}\n"   # 200
curl -sS -X POST http://localhost:8000/transfers \
  -H "Content-Type: application/json" \
  -d '{"idempotencyKey":"check-1","fromWalletId":"00000000-0000-7000-8000-000000000001","toWalletId":"00000000-0000-7000-8000-000000000004","amount":100}'
```

The four seeded wallet ids are always `00000000-0000-7000-8000-00000000000{1,2,3,4}` — there's no
wallet-creation endpoint (deliberately out of scope, ADR-0010), so these are the only wallets that
exist until you seed more.

`just down` stops the stack (the Postgres volume persists; `docker compose down -v` to also wipe
it).

## Option B: app on the host against .venv (fast iteration)

```
just up                       # Postgres only
just migrate                  # or: SEED_DEMO_DATA=true just migrate
just dev                      # uvicorn --reload against .venv
```

Use this when iterating on handler/service code — no Docker image rebuild per change.

## Concurrency demo (CT6)

Once the app is up (either option) and seeded:

```
just simulate                 # or: python scripts/simulate.py --fan-in 100
```

Drives same-wallet fan-in, an opposite-direction pair, and duplicate-idempotency-key replay
against the real running HTTP API, then checks balance reconciliation directly against Postgres.
Prints PASS/FAIL per scenario. This is what proves the 50-100-concurrent-attempt case ADR-0003
deliberately scopes to manual verification rather than the automated CI suite.

## Automated tests instead of manual running

If the goal is "does this change work," `just ci` (lint, format-check, the full pytest suite
including testcontainers-backed repository/E2E tests, and the OpenAPI drift check) is almost
always the faster and more complete answer than manually curling the app. Reach for this skill
specifically when a manual/visual check or the concurrency demo is what's actually being asked
for.

## Common pitfalls

- **"relation wallets does not exist"** — migrations haven't been applied yet. Run `just migrate`
  (or `SEED_DEMO_DATA=true just migrate` if you need the demo wallets).
- **App container can't reach Postgres** — both services must be on `wallet_transfer_net`
  (already wired in `docker-compose.yml`); `just up`/`just demo` handle this automatically.
- **Docker daemon not running** — `just up`, `just demo`, and `just migrate` all need it; check
  with `docker info`.
- **Port 8000 or 5432 already in use** — another instance of this stack, or an unrelated service,
  is already bound to it; `just down` first, or check `docker ps`.

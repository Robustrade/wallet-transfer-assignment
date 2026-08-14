# Wallet Transfer Service

A wallet-to-wallet transfer service with idempotent requests, a double-entry
ledger, and safe concurrent execution. Correctness is enforced at the database
layer (constraints + row locks) rather than in application code.

Full design rationale: [`docs/DESIGN.md`](docs/DESIGN.md).

## Stack

FastAPI · SQLAlchemy 2.0 · Alembic · PostgreSQL · pytest

## Run

```bash
docker compose up --build
```

The app starts on `http://localhost:8000` after running migrations. Seed a
couple of wallets:

```bash
docker compose exec app python -m scripts.seed
```

### Run locally without Docker

```bash
python -m venv .venv && source .venv/bin/activate   # Windows: .venv\Scripts\activate
pip install -r requirements.txt
export DATABASE_URL=postgresql+psycopg2://wallet:wallet@localhost:5432/wallet
alembic upgrade head
python -m scripts.seed
uvicorn app.main:app --reload
```

## API

### `POST /transfers`

```bash
curl -X POST http://localhost:8000/transfers \
  -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
```

`201` on success, `200` on an idempotent replay:

```json
{ "transferId": "…", "status": "PROCESSED", "fromWalletId": "wallet_1", "toWalletId": "wallet_2", "amount": 100 }
```

| HTTP | code | when |
|------|------|------|
| 400 | `INVALID_REQUEST` | missing field, `amount <= 0`, self-transfer |
| 404 | `WALLET_NOT_FOUND` | wallet does not exist |
| 409 | `IDEMPOTENCY_KEY_CONFLICT` | key reused with a different body |
| 422 | `INSUFFICIENT_FUNDS` | source balance too low |

### Reads

- `GET /wallets/{id}` — current balance
- `GET /transfers/{id}` — a single transfer
- `GET /health`

## How correctness is guaranteed

- **Idempotency** — the `idempotency_key` is a primary key; the insert itself is
  the concurrency gate, so a retried request can never create a second transfer.
  A repeat with the same body returns the stored original response.
- **Concurrency** — each transfer runs in one transaction with
  `SELECT … FOR UPDATE` on both wallets, locked in id order to avoid deadlocks.
  A `CHECK (balance >= 0)` constraint backstops against overdraw.
- **Double-entry ledger** — exactly two entries per transfer, enforced by a
  `UNIQUE (transfer_id, type)` constraint.
- **State machine** — `PENDING → PROCESSED | FAILED`, guarded transitions.

## Tests

Tests run against PostgreSQL (the concurrency test needs real row locks). With a
database available at `DATABASE_URL`:

```bash
pytest
```

Covers the happy path, idempotency (replay + conflict), insufficient funds,
validation, and a concurrency test asserting no double-spend under 10 racing
debits.

## AI usage

This solution was built with the help of Claude Code (AI assistant). It was used
to draft the design document, scaffold boilerplate, and cross-check test
coverage. All design decisions — idempotency via unique-insert, deterministic
lock ordering, and pushing invariants into database constraints — were directed
and reviewed by me, and I can walk through any part of the implementation.

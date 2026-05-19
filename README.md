# Wallet Transfer Assignment Repository

This repository is a reusable coding assignment template for evaluating backend engineers on wallet transfers, idempotency, concurrency control, and double-entry ledger design.

## Included

- `ASSIGNMENT.md` - candidate-facing prompt
- `.github/pull_request_template.md` - required PR structure
- `.github/workflows/ci.yml` - lint, format, test placeholder workflow
- `.github/workflows/sonarqube.yml` - SonarQube pull request analysis
- `.github/copilot-instructions.md` - repository-level Copilot review guidance
- `evaluation_guide.md` - reviewer rubric
- `branch-protection-checklist.md` - GitHub setup checklist

## Intended use

1. Mark this repository as a GitHub template repository.
2. Create one private repository per candidate from the template.
3. Add the candidate as a collaborator.
4. Ask them to submit via a pull request into `main`.
5. Enable required checks, SonarQube, and Copilot review in GitHub.

## Notes

- Copilot automatic pull request review is configured in GitHub repository or organization settings, not purely through files in the repo.
- The `copilot-instructions.md` file included here provides repository-specific review guidance once Copilot review is enabled.
- The CI workflow is language-agnostic by default and expects you to set the `LINT_CMD`, `FORMAT_CHECK_CMD`, and `TEST_CMD` repository variables or replace the commands directly.

## How to Submit Assignment

1. **Fork this repository** to your own GitHub account.
2. Complete the assignment described in [`ASSIGNMENT.md`](./ASSIGNMENT.md).
3. **Raise a Pull Request** back to this repository (`main` branch) with your full solution.

Your PR branch should be named: `solution/<your-name>` (e.g., `solution/jane-doe`).

---

## How to run locally

Step-by-step guide if you are new to Go, Docker, or this repo. Use **Git Bash** (Windows) or any terminal on macOS/Linux.

### Prerequisites

| Tool | Purpose | Install |
|------|---------|---------|
| **Go** 1.21+ | Build and run the API | https://go.dev/dl/ |
| **Docker Desktop** | Run PostgreSQL locally | https://www.docker.com/products/docker-desktop/ |
| **Git Bash** (Windows) or terminal | Run commands | Included with Git for Windows |
| **make** (optional) | Shortcuts (`make test`, `make run`) | Git Bash on Windows often includes it; otherwise run the `go` commands below directly |
| **curl** (optional) | Test HTTP API | Usually pre-installed |

#### Fix Go “toolchain not available” on Windows

If you see `go: downloading go1.xx ... toolchain not available`:

```bash
go env -w GOTOOLCHAIN=local
```

Check your installed version:

```bash
GOTOOLCHAIN=local go version
```

You need **1.21 or newer**. Install from https://go.dev/dl/ if older.

### 1. Open the project folder

```bash
cd ~/Kulu/wallet-transfer-assignment
```

All commands below assume you are in this folder (where `docker-compose.yml` and `go.mod` live).

### 2. Configure environment

Copy the example env file (optional; defaults in `Makefile` already work):

```bash
cp .env.example .env
```

For manual commands, export the database URL (PostgreSQL on port **5433**):

```bash
export GOTOOLCHAIN=local
export DATABASE_URL="postgres://wallet:wallet@localhost:5433/wallet_transfer?sslmode=disable"
```

**Why port 5433?** Many machines already run PostgreSQL on `5432`. This project’s Docker DB uses **5433** to avoid conflicts.

### 3. Start PostgreSQL

Start **Docker Desktop**, then:

```bash
make db-up
```

Or without make:

```bash
docker compose up -d postgres
```

Check it is running:

```bash
docker compose ps
```

You should see `wallet-transfer-postgres` with status **Up**.

### 4. Apply database migrations

```bash
make db-migrate
```

Or without make:

```bash
go run -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.3 \
  -path migrations \
  -database "$DATABASE_URL" \
  up
```

Expected output: `1/u init_schema`

Verify tables exist:

```bash
docker compose exec postgres psql -U wallet -d wallet_transfer -c "\dt"
```

### 5. Seed sample wallets

The API needs users and wallets before transfers work.

```bash
docker compose exec -T postgres psql -U wallet -d wallet_transfer <<'SQL'
INSERT INTO users (user_id) VALUES ('user_1'), ('user_2')
ON CONFLICT DO NOTHING;

INSERT INTO wallets (wallet_id, user_id, balance, currency) VALUES
  ('wallet_1', 'user_1', 500, 'USD'),
  ('wallet_2', 'user_2', 100, 'USD')
ON CONFLICT DO NOTHING;
SQL
```

Check balances:

```bash
docker compose exec postgres psql -U wallet -d wallet_transfer -c "SELECT * FROM wallets;"
```

### 6. Run unit tests (no Docker required for tests)

```bash
make test
```

Or:

```bash
GOTOOLCHAIN=local go mod tidy
GOTOOLCHAIN=local go test ./...
```

Coverage report:

```bash
make test-coverage
```

### 7. Start the API server

Use a **new terminal** window, same project folder:

```bash
cd ~/Kulu/wallet-transfer-assignment
export GOTOOLCHAIN=local
export DATABASE_URL="postgres://wallet:wallet@localhost:5433/wallet_transfer?sslmode=disable"
make run
```

Or:

```bash
GOTOOLCHAIN=local go run ./cmd/server
```

You should see: `listening on :8080`

Keep this terminal open while testing.

### 8. Test the HTTP API

In another terminal:

**Health check**

```bash
curl -s http://localhost:8080/health
```

Expected: `{"status":"ok"}`

**Create a transfer**

```bash
curl -s -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "demo-1",
    "fromWalletId": "wallet_1",
    "toWalletId": "wallet_2",
    "amount": 100
  }'
```

Expected: JSON with `transferId`, `"state":"PROCESSED"`, `amount`, `currency`.

**Idempotency (run the same curl again)**

The same `transferId` should be returned and balances must **not** debit twice.

**Insufficient funds (should fail)**

```bash
curl -s -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "demo-fail",
    "fromWalletId": "wallet_1",
    "toWalletId": "wallet_2",
    "amount": 999999
  }'
```

### 9. Verify data in PostgreSQL

**Important:** run from the project folder (where `docker-compose.yml` is).

```bash
cd ~/Kulu/wallet-transfer-assignment
docker compose exec postgres psql -U wallet -d wallet_transfer
```

Then run:

```sql
SELECT wallet_id, balance FROM wallets ORDER BY wallet_id;
SELECT transfer_id, state, amount FROM transfers;
SELECT wallet_id, transfer_id, type, amount FROM ledger_entries ORDER BY entry_id;
SELECT idempotency_key, transfer_id FROM idempotency_records;
```

Type `\q` to quit.

After a successful transfer of **100** from `wallet_1` (500) to `wallet_2` (100):

- `wallet_1` balance → **400**
- `wallet_2` balance → **200**
- **2** ledger rows (DEBIT + CREDIT)
- **1** idempotency row for `demo-1`

### Makefile quick reference

| Command | What it does |
|---------|----------------|
| `make db-up` | Start Postgres in Docker |
| `make db-down` | Stop containers |
| `make db-migrate` | Apply SQL migrations |
| `make db-reset` | Roll back and re-apply latest migration |
| `make run` | Start HTTP server on `:8080` |
| `make test` | Run all Go tests |
| `make test-coverage` | Tests + coverage summary |

### Troubleshooting

**`no configuration file provided: not found`**

You are not in the project directory. Run:

```bash
cd ~/Kulu/wallet-transfer-assignment
```

**`password authentication failed for user "wallet"`**

You are connecting to the wrong Postgres (often port **5432**). Use **5433** in `DATABASE_URL`.

**`connection refused` on database**

- Is Docker Desktop running?
- Run `make db-up` and `docker compose ps`

**`migrate: command not found`**

Use `make db-migrate` (runs migrate via `go run`).

**Port 5433 already in use**

```bash
WALLET_DB_HOST_PORT=5434 make db-up
export DATABASE_URL="postgres://wallet:wallet@localhost:5434/wallet_transfer?sslmode=disable"
```

**Reset everything (fresh database)**

```bash
docker compose down -v
make db-up
make db-migrate
# seed wallets again (step 5)
```

### Project layout

```text
cmd/server/           → main entrypoint (starts HTTP server)
internal/handler/     → HTTP routes (POST /transfers, GET /health)
internal/service/     → business logic and transfer workflow
internal/repository/  → database access (PostgreSQL)
internal/domain/      → entities and domain rules
migrations/           → SQL schema
docker-compose.yml    → local PostgreSQL
```

See [`implementation-details.md`](./implementation-details.md) for design notes and [`ASSIGNMENT.md`](./ASSIGNMENT.md) for requirements.

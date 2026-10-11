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

## Local development

The service uses Go 1.24 or newer and PostgreSQL. It uses Go's `net/http` and `database/sql` packages with the pgx PostgreSQL driver; no HTTP framework is used.

1. Create a PostgreSQL database and a user that can create tables in it.
2. Set `DATABASE_URL` in your shell. `.env.example` documents the expected values; the application does not load `.env` files.
3. From the repository root, apply pending migrations:

   ```powershell
   $env:DATABASE_URL = "postgres://wallet_user:wallet_password@localhost:5432/wallet_transfer?sslmode=disable"
   go run ./cmd/migrate
   ```

4. Start the HTTP server:

   ```powershell
   go run ./cmd/server
   ```

The server listens on `:8080` by default. Set `HTTP_ADDR` to change the address. `GET /healthz` checks the PostgreSQL connection. The migration command records applied files in `schema_migrations` and applies each file in a transaction; run it from the repository root.

### Create a transfer

The assignment does not require a wallet-creation endpoint, so provision wallets in PostgreSQL first. For example, run this in your PostgreSQL client:

```sql
INSERT INTO wallets (id, balance)
VALUES ('wallet_1', 10000), ('wallet_2', 5000);
```

Then, from PowerShell:

```powershell
$body = @{
  idempotencyKey = "transfer-demo-001"
  fromWalletId = "wallet_1"
  toWalletId = "wallet_2"
  amount = 100
} | ConvertTo-Json

Invoke-RestMethod -Method Post -Uri "http://localhost:8080/transfers" `
  -ContentType "application/json" -Body $body
```

The amount is an integer in minor currency units. Repeating the same body with the same idempotency key returns the saved response. Reusing the key with different transfer details returns `409 Conflict`.

### Tests

Run the unit and HTTP handler tests with:

```powershell
go test ./...
```

The PostgreSQL integration tests run too when `TEST_DATABASE_URL` points to a database where the migrations have been applied. They create uniquely named wallets and remove only their own records. For example, using a dedicated test database:

```powershell
$env:TEST_DATABASE_URL = "postgres://wallet_user:wallet_password@localhost:5432/wallet_transfer?sslmode=disable"
go test ./...
```

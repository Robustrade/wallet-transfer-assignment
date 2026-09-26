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

## Wallet Transfer Service

This repository contains a Go wallet transfer service backed by PostgreSQL. See [DESIGN.md](./DESIGN.md) for the API contract, transaction/idempotency decisions, consistency guarantees, and failure behavior.

### Run

Requires Go 1.24 or newer and a running PostgreSQL instance. Copy the sample configuration if using Docker Compose:

```powershell
Copy-Item .env.example .env
docker compose up -d --build
```

The API listens on `http://localhost:8080`. Compose starts PostgreSQL and the API. To run the Go server directly on the host instead, set its environment explicitly in PowerShell and start PostgreSQL separately:

```powershell
$env:DATABASE_URL = "postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable"
$env:PORT = "8080"
$env:SSL_ENABLED = "false"
$env:MAX_CLAIM_ATTEMPTS = "3"
go run ./cmd/server
```

For HTTPS, set `SSL_ENABLED` to `true`. Provide both `TLS_CERT_FILE` and `TLS_KEY_FILE`, or omit both to have the service generate a development self-signed localhost certificate. Remove session variables when finished:

```powershell
Remove-Item Env:DATABASE_URL -ErrorAction SilentlyContinue
Remove-Item Env:PORT -ErrorAction SilentlyContinue
Remove-Item Env:SSL_ENABLED -ErrorAction SilentlyContinue
Remove-Item Env:MAX_CLAIM_ATTEMPTS -ErrorAction SilentlyContinue
Remove-Item Env:TLS_CERT_FILE -ErrorAction SilentlyContinue
Remove-Item Env:TLS_KEY_FILE -ErrorAction SilentlyContinue
```

Create wallets with opening balances in integer minor units:

```powershell
Invoke-RestMethod -Method Post -Uri http://localhost:8080/wallets -ContentType 'application/json' -Body '{"walletId":"wallet_1","initialBalance":10000}'
Invoke-RestMethod -Method Post -Uri http://localhost:8080/wallets -ContentType 'application/json' -Body '{"walletId":"wallet_2","initialBalance":0}'
```

Create a transfer; send the same key and request again to receive the original result:

```powershell
Invoke-RestMethod -Method Post -Uri http://localhost:8080/transfers -ContentType 'application/json' -Body '{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
```

`GET /` returns the JSON endpoint index. Get a balance with `GET /wallets/{walletId}`, transfer history with `GET /wallets/{walletId}/transfers?limit=50`, and service health with `GET /healthz`. Transfer amounts are positive integers in minor currency units; insufficient funds is stored and replayable.

### Test

```powershell
make test
go test -race ./...
```

The PostgreSQL integration tests run with Testcontainers by default and require Docker:

```powershell
make test-integration
```

Set `TEST_DATABASE_URL` to use an existing PostgreSQL instance instead. In PowerShell, for example:

```powershell
$env:TEST_DATABASE_URL = "postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable"
make test-integration-db
```

The `postgres-integration` GitHub Actions workflow provisions PostgreSQL as a hosted service and runs the integration suite without relying on a Docker daemon inside the job. Other useful targets are `make up`, `make down`, `make fmt-check`, and `make lint`. See [AI_USAGE.md](./AI_USAGE.md) for the AI-use disclosure and prompts from this work session.

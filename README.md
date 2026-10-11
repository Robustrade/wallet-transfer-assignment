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
2. Copy `.env.example` to `.env`, then set `DATABASE_URL` for your database. The application reads environment variables directly; load `.env` in your shell or IDE because no dotenv library is included.
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

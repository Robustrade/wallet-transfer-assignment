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

## Solution: Running Locally

This branch's implementation lives under `cmd/`, `internals/`, and `db/`. Design notes are in [`DESIGN.md`](./DESIGN.md).

### 1. Start Postgres

```bash
docker compose up -d
```

This starts Postgres 18 on `localhost:5432` and applies the schema in
[`db/init/001_schema.sql`](./db/init/001_schema.sql) automatically on first
start (via the image's `/docker-entrypoint-initdb.d` convention). The
container's user/password/db (`postgres`/`root`/`wallet_schema_db`) match the
`DATABASE_URL` already committed in `.env`, so no further config is needed.

To re-apply the schema after editing it, drop the volume first:

```bash
docker compose down -v
docker compose up -d
```

If port 5432 is already taken by another Postgres instance, override it:

```bash
POSTGRES_PORT=5433 docker compose up -d
```

Then point `DATABASE_URL` at that port instead.

### 2. Run the API

```bash
go run ./cmd/api
```

Listens on `:8080` (override with the `PORT` env var). `POST /transfers` is the only route.

### 3. Run the tests

```bash
go test ./...
```

Model, service, and handler packages have unit tests that run without a
database. `internals/tests/` holds integration tests that exercise real
Postgres locking/concurrency behavior — they skip automatically if
`DATABASE_URL` isn't set, and run against whatever `docker compose up -d`
started otherwise.

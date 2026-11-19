# Wallet Transfer Service (Java solution)

Wallet-to-wallet transfers with idempotent requests, a double-entry ledger and concurrency-safe
balances. Java 21, Spring Boot 3, PostgreSQL, Gradle. The design, contract and reasoning are in
[`docs/design.md`](./docs/design.md).

## Requirements

- JDK 21
- Docker (the tests start a real PostgreSQL through Testcontainers, and `docker compose` provides one
  for local runs)

## Run

```bash
docker compose up -d     # PostgreSQL on :5432 (wallet / wallet / wallet)
./gradlew bootRun        # API on :8080, schema applied by Flyway on start-up
```

Connection settings come from `DB_URL`, `DB_USER` and `DB_PASSWORD` (defaults match the compose file).

```bash
curl -H 'Content-Type: application/json' -d '{"id":"alice","openingBalance":100}' localhost:8080/wallets
curl -H 'Content-Type: application/json' -d '{"id":"bob","openingBalance":0}'     localhost:8080/wallets
curl -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"k1","fromWalletId":"alice","toWalletId":"bob","amount":30}' \
  localhost:8080/transfers
```

| Endpoint | Purpose |
|----------|---------|
| `POST /transfers` | 201 processed, 200 replay (`Idempotent-Replayed: true`), 422 failed (insufficient funds), 400, 404, 409 |
| `GET /transfers/{id}` | fetch a transfer |
| `POST /wallets` | create a wallet with an opening balance |
| `GET /wallets/{id}` | read the current balance |
| `GET /actuator/health` | health, including the database |

## Test and quality gates

```bash
./gradlew check          # formatting, Checkstyle, PMD, SpotBugs and all tests
./gradlew spotlessApply  # fix formatting
```

`check` runs Spotless (google-java-format), Checkstyle, PMD, SpotBugs, the unit tests and the
integration tests (domain, repositories, services, concurrency, HTTP) against a real PostgreSQL.
With Colima, point Testcontainers at the VM socket:
`DOCKER_HOST=unix://$HOME/.colima/default/docker.sock TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock`.

### CI configuration

`.github/workflows/ci.yml` runs whatever the repository variables say. Suggested values:

| Variable | Value |
|----------|-------|
| `LINT_CMD` | `./gradlew checkstyleMain checkstyleTest pmdMain pmdTest spotbugsMain spotbugsTest` |
| `FORMAT_CHECK_CMD` | `./gradlew spotlessCheck` |
| `TEST_CMD` | `./gradlew test` |

The runner needs Docker for Testcontainers.

---

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

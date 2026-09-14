# Java / Spring Boot skeleton

Open this folder in IntelliJ as a Maven project (`pom.xml`).

## Prerequisites

- JDK 17+
- Docker (for local PostgreSQL)

On this machine:

```bash
export JAVA_HOME=/opt/homebrew/opt/openjdk@17
```

## PostgreSQL

```bash
docker compose up -d
```

Defaults: `localhost:5432`, database/user/password = `wallet_transfer` / `wallet` / `wallet`

## Schema (Flyway)

On first app start, Flyway applies `src/main/resources/db/migration/V1__init_schema.sql`
and records it in `flyway_schema_history`. Later starts skip already-applied migrations.
Hibernate `ddl-auto` is `none` so the schema is not recreated by JPA.

## Run

1. Start Colima + Postgres (if not already running):
   ```bash
   colima start
   docker compose up -d
   ```
2. IntelliJ: run `WalletTransferApplication`.

Or:

```bash
./mvnw spring-boot:run
```

## Test / build

Unit and integration tests run automatically as part of the Maven build (Surefire):

```bash
# Full service build: compile + unit tests + integration tests + package
./mvnw clean verify

# Same test suite (without packaging)
./mvnw test
```

Tests use in-memory H2 (`test` profile), so Docker is not required for `./mvnw test` / `./mvnw verify`.

Optional filters:

```bash
# Unit tests only
./mvnw -Dtest='!*IntegrationTest' test

# Integration tests only
./mvnw -Dtest='*IntegrationTest' test
```

## Live API smoke suite (Python)

Against a running service (Postgres + jar):

```bash
docker compose up -d
java -jar target/wallet-transfer-0.0.1-SNAPSHOT.jar
# other terminal:
python3 scripts/api_integration_tests.py --base-url http://127.0.0.1:8080
```

Writes a markdown report to `reports/api_integration_report.md`.

## Metrics

Actuator Prometheus scrape: `GET /actuator/prometheus`  
See [`docs/METRICS.md`](docs/METRICS.md).

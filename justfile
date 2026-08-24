src := "src/wallet_transfer"
flyway_image := "flyway/flyway:10-alpine"

# Install dev dependencies
install:
    pip install -r requirements.txt -r requirements-dev.txt

# Apply schema migrations via Flyway against the docker-compose Postgres.
# SEED_DEMO_DATA=true also applies db/seed/ (ADR-0010) — unset by default everywhere,
# including CI and testcontainers-backed tests.
migrate:
    #!/usr/bin/env bash
    set -euo pipefail
    locations="filesystem:/flyway/sql"
    volumes=(-v "{{justfile_directory()}}/db/migrations:/flyway/sql:ro")
    if [ "${SEED_DEMO_DATA:-}" = "true" ]; then
        locations="${locations},filesystem:/flyway/sql_seed"
        volumes+=(-v "{{justfile_directory()}}/db/seed:/flyway/sql_seed:ro")
    fi
    docker run --rm --network wallet_transfer_net "${volumes[@]}" {{flyway_image}} \
        -url=jdbc:postgresql://postgres:5432/wallet_transfer \
        -user=wallet -password=wallet \
        -connectRetries=10 \
        "-locations=${locations}" \
        migrate

# Start the docker-compose Postgres and wait for it to accept connections.
up:
    #!/usr/bin/env bash
    set -euo pipefail
    docker compose up -d postgres
    until docker compose exec -T postgres pg_isready -U wallet -d wallet_transfer >/dev/null 2>&1; do
        sleep 1
    done

# Stop the docker-compose services. The Postgres data volume persists (docker compose down -v
# to also remove it).
down:
    docker compose down

# Run the FastAPI app locally against .venv, for fast dev iteration — requires `just up` and
# `just migrate` first.
dev:
    uvicorn wallet_transfer.handlers.app:create_app --factory --reload --port 8000

# Full containerized demo stack (ADR-0003's interview demo): Postgres + schema + demo wallets
# (ADR-0010) + the app itself, all via docker-compose.
demo: up
    SEED_DEMO_DATA=true just migrate
    docker compose up -d --build app

# Drive scripts/simulate.py's concurrent load against a running app (see `just demo`).
simulate:
    python scripts/simulate.py

# Regenerate the handler-layer Pydantic models from openapi/spec.yaml (ADR-0005).
# Never hand-edit the output — it's a pure build artifact, regenerated on every spec change.
generate-models:
    datamodel-codegen \
        --input openapi/spec.yaml \
        --input-file-type openapi \
        --output {{src}}/handlers/generated_models.py \
        --output-model-type pydantic_v2.BaseModel \
        --target-python-version 3.12 \
        --use-schema-description \
        --use-annotated \
        --disable-timestamp

# Lint (ruff + mypy + import-linter, once src/ exists)
lint:
    ruff check .
    @if [ -d {{src}} ]; then mypy src; else echo "mypy: {{src}} not created yet, skipping"; fi
    @if [ -d {{src}} ]; then lint-imports; else echo "import-linter: {{src}} not created yet, skipping"; fi

# Check formatting without modifying files
format-check:
    ruff format --check .

# Auto-format and fix lint issues
fmt:
    ruff format .
    ruff check --fix .

# Run the test suite
test:
    pytest

# OpenAPI drift check (ADR-0005, E2) — required CI gate, not a pytest case.
check-openapi-drift:
    python scripts/check_openapi_drift.py

# Run tests with coverage, once src/ exists
test-cov:
    @if [ -d {{src}} ]; then pytest --cov=wallet_transfer --cov-report=term-missing; else echo "coverage: {{src}} not created yet, skipping"; fi

# Install the pre-commit git hook
pre-commit-install:
    pre-commit install

# Full CI gate: lint, format-check, test, OpenAPI drift
ci: lint format-check test check-openapi-drift

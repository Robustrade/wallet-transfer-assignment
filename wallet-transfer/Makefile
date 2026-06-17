.PHONY: tidy db-up db-down migrate run test

DB_NAME    := wallet
DB_USER    := postgres
DB_PASS    := postgres
DB_PORT    := 5432
DATABASE_URL := postgres://$(DB_USER):$(DB_PASS)@localhost:$(DB_PORT)/$(DB_NAME)?sslmode=disable

tidy:
	go mod tidy

# Start a Postgres container (idempotent: skips if already running).
db-up:
	@if docker ps --filter name=wallet-pg --format '{{.Names}}' | grep -q wallet-pg; then \
		echo "wallet-pg already running"; \
	else \
		docker run -d --name wallet-pg \
			-e POSTGRES_USER=$(DB_USER) \
			-e POSTGRES_PASSWORD=$(DB_PASS) \
			-e POSTGRES_DB=$(DB_NAME) \
			-p $(DB_PORT):5432 \
			postgres:16-alpine; \
		echo "waiting for postgres..."; \
		until docker exec wallet-pg pg_isready -U $(DB_USER) -q; do sleep 1; done; \
		echo "postgres ready"; \
	fi

# Stop and remove the Postgres container.
db-down:
	docker rm -f wallet-pg 2>/dev/null || true

# Apply the embedded SQL migrations and exit (no server started).
migrate:
	DATABASE_URL=$(DATABASE_URL) go run ./cmd/migrate

# Start the HTTP server (also applies migrations on startup).
run:
	DATABASE_URL=$(DATABASE_URL) go run ./cmd/server

# Tests spin up their own Postgres via testcontainers (Docker required).
# Or set WALLET_TEST_DATABASE_URL to use an existing database.
test:
	go test ./... -race -count=1

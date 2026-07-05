.PHONY: build run test test-integration fmt fmt-write lint tidy \
	docker-up docker-down docker-logs \
	migrate-create migrate-up migrate-down

build:
	go build -o bin/server ./cmd/server

run:
	go run ./cmd/server

# Unit tests only (domain, etc). No external dependencies required.
test:
	go test ./...

# Integration tests (service/repository) tagged `//go:build integration`.
# Requires a migrated Postgres reachable via the DB_* env vars (defaults
# match docker-compose): `docker compose up -d postgres && make migrate-up`.
# -p 1 forces packages to run sequentially: repository/service integration
# tests share one physical DB (truncate-then-seed per test), so letting Go
# run those two test binaries as parallel OS processes causes cross-package
# TRUNCATE/INSERT races (duplicate keys, even real Postgres deadlocks).
test-integration:
	go test -tags=integration -p 1 ./...

# Fails (non-zero exit) if any file is not gofmt'd -- gofmt -l alone only
# lists offenders and always exits 0, so it can't gate CI by itself.
fmt:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "The following files are not gofmt'd:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

# Applies gofmt in place.
fmt-write:
	gofmt -w .

lint:
	golangci-lint run

tidy:
	go mod tidy

docker-up:
	docker compose up --build

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f

# Usage: make migrate-create NAME=create_wallets_table
migrate-create:
	docker run --rm -v $(CURDIR)/migrations:/migrations migrate/migrate:v4.17.1 \
		create -ext sql -dir /migrations -seq $(NAME)

migrate-up:
	docker compose --profile tools run --rm migrate up

migrate-down:
	docker compose --profile tools run --rm migrate down 1

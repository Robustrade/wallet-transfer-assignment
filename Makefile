.PHONY: run migrate lint fmt-check test test-integration seed

run:
	docker compose up -d postgres redis
	go run ./cmd/server

migrate:
	go run -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate -path migrations -database "$${POSTGRES_DSN}" up

lint:
	golangci-lint run ./...

fmt-check:
	@test -z "$$(gofmt -l .)" || (echo "The following files are not gofmt'd:"; gofmt -l .; exit 1)

test:
	go test ./...

# Integration tests run against postgres+redis brought up by docker
# compose, not testcontainers — testcontainers' docker-client dependency
# chain forces go1.25+, which conflicts with this project's go1.24
# target. TEST_POSTGRES_DSN/TEST_REDIS_ADDR default to docker-compose.yml's
# ports, override for CI.
test-integration:
	docker compose up -d postgres redis
	TEST_POSTGRES_DSN="postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable" \
	TEST_REDIS_ADDR="localhost:6379" \
	go test -race -tags=integration ./...

seed:
	docker compose up -d postgres
	docker compose exec -T postgres psql -U wallet -d wallet < scripts/seed.sql

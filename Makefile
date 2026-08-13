# Load PORT/DATABASE_URL from .env if present (see .env.example) and export
# them to every recipe below. -include (not include) so this is a no-op,
# not an error, when .env doesn't exist -- config.Load()'s own defaults
# then apply instead, which match .env.example's values.
-include .env
export

.PHONY: db-up db-down run build test test-unit test-integration lint fmt-check

db-up:
	docker compose up -d postgres

db-down:
	docker compose down

run: db-up
	go run ./cmd/server

build:
	go build -o bin/server ./cmd/server

test-unit:
	go test ./internal/service/... -v

test-integration: db-up
	INTEGRATION_DATABASE_URL=postgres://wallet:wallet@localhost:5432/wallet_transfer?sslmode=disable \
		go test -tags=integration ./test/... -v -count=1

test: test-unit test-integration

lint:
	go vet ./...

fmt-check:
	test -z "$$(gofmt -l .)"

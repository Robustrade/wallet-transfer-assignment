.PHONY: run test test-integration test-integration-db fmt fmt-check lint up down

run:
	go run ./cmd/server

test:
	go test ./... -count=1

test-integration:
	go test -tags=integration ./internal/repository/postgres -count=1 -v

test-integration-db:
	@test -n "$(TEST_DATABASE_URL)" || (echo "Set TEST_DATABASE_URL to a PostgreSQL connection string" && exit 1)
	go test -tags=integration ./internal/repository/postgres -count=1 -v

fmt:
	gofmt -w $$(find cmd internal -name '*.go')

fmt-check:
	@test -z "$$(gofmt -l cmd internal)"

lint:
	golangci-lint run ./...

up:
	docker compose up -d --build

down:
	docker compose down
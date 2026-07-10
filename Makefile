.PHONY: run migrate lint fmt-check test test-integration

run:
	docker-compose up -d postgres redis
	go run ./cmd/server

migrate:
	go run -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate -path migrations -database "$${POSTGRES_DSN}" up

lint:
	golangci-lint run ./...

fmt-check:
	@test -z "$$(gofmt -l .)" || (echo "The following files are not gofmt'd:"; gofmt -l .; exit 1)

test:
	go test ./...

test-integration:
	go test -tags=integration ./...

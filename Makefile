.PHONY: build run lint fmt fmt-check test test-race

build:
	go build ./...

run:
	go run ./cmd/server

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .

# Fails (and lists the offending files) when anything is not gofmt-formatted.
fmt-check:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }

# Integration tests start an embedded PostgreSQL automatically; set
# TEST_DATABASE_URL to use an existing server instead.
test:
	go test ./...

test-race:
	go test -race ./...

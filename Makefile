.PHONY: all build test test-concurrency run clean docker-up docker-down fmt lint

all: fmt lint test build

build:
	go build -o bin/server ./cmd/api/main.go

run:
	go run ./cmd/api/main.go

test:
	go test -v -race ./...

test-coverage:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

fmt:
	go fmt ./...

lint:
	go vet ./...

clean:
	rm -rf bin/ coverage.out *.db *.db-shm *.db-wal

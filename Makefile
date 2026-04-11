build:
	go build ./cmd/server

test:
	go test ./... -race -cover

lint:
	golangci-lint run ./...

fmt-check:
	@test -z "$$(gofmt -l .)" || (echo "formatting issues:"; gofmt -l .; exit 1)

.PHONY: build test lint fmt-check

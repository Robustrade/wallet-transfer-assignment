.PHONY: build run test lint fmt-check fmt clean

# Build the server binary
build:
    CGO_ENABLED=1 go build -o bin/server ./cmd/server

# Run the server
run: build
    ./bin/server

# Run all tests (no external dependencies needed)
test:
    CGO_ENABLED=1 go test ./... -v -timeout 60s

# Lint (requires golangci-lint)
lint:
    golangci-lint run ./...

# Format check
fmt-check:
    @test -z "$$(gofmt -l .)" || (echo "Files need formatting:" && gofmt -l . && exit 1)

# Format
fmt:
    gofmt -w .

# Clean build artifacts
clean:
    rm -rf bin/ wallet_transfer.db
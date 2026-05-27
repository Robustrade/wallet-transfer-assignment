.PHONY: fmt lint test run

fmt:
	gofmt -w cmd internal

lint:
	go vet ./...

test:
	go test ./...

run:
	go run ./cmd/wallet-transfer

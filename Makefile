.PHONY: build run test fmt vet lint

build:
	go build ./...

run:
	go run ./cmd/server

test:
	go test ./... -v

fmt:
	go fmt ./...

vet:
	go vet ./...

lint: vet
	golangci-lint run ./...

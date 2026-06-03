.PHONY: run test format lint

run:
	go run cmd/server/main.go

test:
	go test -v -race ./...

format:
	go fmt ./...

lint:
	golangci-lint run ./...

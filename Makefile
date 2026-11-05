.PHONY: run test lint fmt fmt-check db-up

db-up:
	docker compose up -d --wait

run:
	go run ./cmd/server

test:
	go test ./... -race -cover

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .

fmt-check:
	test -z "$$(gofmt -l .)"

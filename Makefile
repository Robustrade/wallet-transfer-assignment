.PHONY: up down logs test lint fmt fmt-check

up:
	docker compose up --build

down:
	docker compose down -v

logs:
	docker compose logs -f --tail=200

test:
	go test ./... -race -cover

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .

fmt-check:
	test -z "$$(gofmt -l .)"


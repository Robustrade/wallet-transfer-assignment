.PHONY: test test-race integration

test:
	go test ./...

test-race:
	go test -race ./...

integration:
	docker compose up -d postgres
	go test ./internal/integration -v

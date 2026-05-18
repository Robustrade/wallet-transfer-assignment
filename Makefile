APP_NAME=wallet-transfer-service
DATABASE_URL=postgres://wallet_user:wallet_password@localhost:5432/wallet_transfer?sslmode=disable
TEST_DATABASE_URL?=$(DATABASE_URL)
HTTP_ADDR=:8080

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: test
test:
	go test ./...

.PHONY: test-integration
test-integration:
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test ./tests -run 'TestCreateTransfer_Postgres' -count=1

.PHONY: docker-up
docker-up:
	docker compose up -d

.PHONY: docker-down
docker-down:
	docker compose down

.PHONY: docker-reset
docker-reset:
	docker compose down -v
	docker compose up -d

.PHONY: run
run:
	DATABASE_URL="$(DATABASE_URL)" HTTP_ADDR="$(HTTP_ADDR)" go run ./cmd/server

.PHONY: curl-transfer
curl-transfer:
	curl -i -X POST http://localhost:8080/transfers \
		-H "Content-Type: application/json" \
		-d '{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'

.PHONY: curl-transfer-replay
curl-transfer-replay:
	curl -i -X POST http://localhost:8080/transfers \
		-H "Content-Type: application/json" \
		-d '{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'

.PHONY: curl-transfer-conflict
curl-transfer-conflict:
	curl -i -X POST http://localhost:8080/transfers \
		-H "Content-Type: application/json" \
		-d '{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_3","amount":500}'

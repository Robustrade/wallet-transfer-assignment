-include .env
export

BINARY_NAME=wallet
BUILD_DIR=bin

all: clean build run

build:
	mkdir -p $(BUILD_DIR)
	go build -o ./$(BUILD_DIR)/$(BINARY_NAME) ./cmd/main.go

run:
	./$(BUILD_DIR)/$(BINARY_NAME)

clean:
	rm -rf $(BUILD_DIR)

format:
	gofmt -w .

test:
	go test ./...

mock-gen:
	mockgen -source=internal/wallet/repository/transfer.go -destination=internal/wallet/repository/mock/mock.go -package=mock_repository
	mockgen -source=internal/wallet/service/transfer.go -destination=internal/wallet/service/mock/mock.go -package=mock_service

migrate-up:
	migrate -path db/migrations -database "$(DB_URL)" up

migrate-down:
	migrate -path db/migrations -database "$(DB_URL)" down
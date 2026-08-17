-include .env
export

test:
	go test ./...

mock-gen:
	mockgen -source=internal/wallet/repository/transfer.go -destination=internal/wallet/repository/mock/mock.go -package=mock_repository
	mockgen -source=internal/wallet/service/transfer.go -destination=internal/wallet/service/mock/mock.go -package=mock_service

migrate-up:
	migrate -path db/migrations -database "$(DB_URL)" up

migrate-down:
	migrate -path db/migrations -database "$(DB_URL)" down
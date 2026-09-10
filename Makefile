## makefile
DATABASE_URL ?= postgres://wallet_user:wallet_password@localhost:5432/wallet_transfer?sslmode=disable

export DATABASE_URL
.PHONY: setup db-up db-down seed clean run

setup: db-up
	@echo "Setup complete"
	@echo "DATABASE_URL=$(DATABASE_URL)"

db-up:
	docker compose up -d

db-down:
	docker compose down

seed:
	docker compose exec -T postgres psql \
		-U wallet_user \
		-d wallet_transfer \
		-c "INSERT INTO wallets (id, balance) VALUES \
		('11111111-1111-1111-1111-111111111111', 500), \
		('22222222-2222-2222-2222-222222222222', 500) \
		ON CONFLICT (id) DO UPDATE SET balance = EXCLUDED.balance;"

clean:
	docker compose exec -T postgres psql \
		-U wallet_user \
		-d wallet_transfer \
		-c "TRUNCATE TABLE ledger_entries, transfers, idempotency_records RESTART IDENTITY CASCADE; \
		UPDATE wallets SET balance = 500 \
		WHERE id IN ( \
		'11111111-1111-1111-1111-111111111111', \
		'22222222-2222-2222-2222-222222222222' \
		);"

run:
	go run ./cmd/server
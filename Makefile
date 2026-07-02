.PHONY: db-up db-down db-logs migrate-up migrate-down migrate-version

DATABASE_URL ?= postgres://wallet:wallet@localhost:5432/wallet_transfer?sslmode=disable
MIGRATE_IMAGE ?= migrate/migrate:v4.18.1

db-up:
	docker compose up -d postgres

db-down:
	docker compose down

db-logs:
	docker compose logs -f postgres

migrate-up:
	docker compose run --rm migrate

migrate-down:
	docker run --rm \
		-v "$(CURDIR)/migrations:/migrations" \
		--add-host=host.docker.internal:host-gateway \
		$(MIGRATE_IMAGE) \
		-path=/migrations \
		-database="postgres://wallet:wallet@host.docker.internal:5432/wallet_transfer?sslmode=disable" \
		down 1

migrate-version:
	docker run --rm \
		-v "$(CURDIR)/migrations:/migrations" \
		--add-host=host.docker.internal:host-gateway \
		$(MIGRATE_IMAGE) \
		-path=/migrations \
		-database="postgres://wallet:wallet@host.docker.internal:5432/wallet_transfer?sslmode=disable" \
		version

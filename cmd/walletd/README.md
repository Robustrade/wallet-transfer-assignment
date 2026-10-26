# Wallet server

Run `go run ./cmd/walletd` after applying the SQL migration to a development database.

Environment: `DATABASE_URL` is required; `HTTP_ADDR` defaults to `:8080`.
The process pings PostgreSQL and verifies three required tables at startup, handles SIGINT/SIGTERM, and uses a 10-second
graceful HTTP shutdown. It intentionally does **not** auto-migrate the database.

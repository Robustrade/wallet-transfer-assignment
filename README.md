# Wallet Transfer Service

A small Go service for transferring funds between wallets with transactional
consistency, durable idempotency, and a double-entry ledger.

## Tech Stack

- Go
- PostgreSQL 16
- Docker / Docker Compose
- pgx v5
- Go standard library HTTP server
- Go `testing` package

## Architecture

```text
HTTP Handler
     |
     v
Transfer Service
     |
     v
Repository
     |
     v
PostgreSQL
```
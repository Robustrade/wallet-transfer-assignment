# Task — Scaffold Domain-Driven Structure

## Cleanup
- [x] Remove old empty internal packages (db, handler, router, service)

## Domain: wallet
- [x] internal/domain/wallet/model.go
- [x] internal/domain/wallet/repository.go
- [x] internal/domain/wallet/service.go
- [x] internal/domain/wallet/handler.go

## Domain: transfer
- [x] internal/domain/transfer/model.go
- [x] internal/domain/transfer/repository.go
- [x] internal/domain/transfer/service.go
- [x] internal/domain/transfer/handler.go

## Domain: ledger
- [x] internal/domain/ledger/model.go
- [x] internal/domain/ledger/repository.go
- [x] internal/domain/ledger/service.go
- [x] internal/domain/ledger/handler.go

## Platform: postgres
- [ ] internal/platform/postgres/connection.go
- [ ] internal/platform/postgres/migrations/001_create_wallets.sql
- [ ] internal/platform/postgres/migrations/002_create_transfers.sql
- [ ] internal/platform/postgres/migrations/003_create_ledger_entries.sql
- [ ] internal/platform/postgres/wallet_repo.go
- [ ] internal/platform/postgres/transfer_repo.go
- [ ] internal/platform/postgres/ledger_repo.go

## Router & Wiring
- [ ] internal/router/router.go
- [ ] server/main.go (update wiring)

## Tests
- [ ] tests/testhelper/db.go
- [ ] tests/transfer_test.go
- [ ] tests/concurrency_test.go

# Build TODO

## Project Setup

- [x] initialize Go module
- [x] choose router and database libraries
- [x] add format and test commands to README
- [ ] fix CI command setup if needed

## Database

- [x] create wallet schema
- [x] create transfer schema
- [x] create ledger schema
- [x] create idempotency schema
- [x] add constraints and indexes

## API

- [x] `GET /health`
- [x] `POST /wallets`
- [x] `GET /wallets/{walletId}`
- [x] `POST /transfers`
- [x] `GET /transfers/{transferId}`
- [x] `GET /wallets/{walletId}/ledger`

## Core Logic

- [x] implement transfer transaction
- [x] implement idempotency replay
- [x] implement concurrent idempotency-key race handling
- [x] implement row locking
- [x] implement ledger writes
- [x] implement failure handling

## Tests

- [x] successful transfer
- [x] insufficient funds
- [x] idempotent retry
- [x] ledger correctness
- [x] concurrency safety

## Documentation

- [x] update README with setup instructions
- [x] write PR notes for schema, idempotency, concurrency, and AI usage
- [x] add AI usage notes

## Optional Future Work

- [ ] persist rejected transfer attempts as `FAILED` when audit requirements need it
- [ ] add structured request logging
- [ ] add metrics

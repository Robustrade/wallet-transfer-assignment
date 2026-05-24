# Build TODO

## Project Setup

- [ ] initialize Go module
- [ ] choose router and database libraries
- [ ] add lint, format, and test commands
- [ ] fix CI command setup if needed

## Database

- [ ] create wallet schema
- [ ] create transfer schema
- [ ] create ledger schema
- [ ] create idempotency schema
- [ ] add constraints and indexes

## API

- [ ] `GET /health`
- [ ] `POST /wallets`
- [ ] `GET /wallets/{walletId}`
- [ ] `POST /transfers`
- [ ] `GET /transfers/{transferId}`
- [ ] `GET /wallets/{walletId}/ledger`

## Core Logic

- [ ] implement transfer transaction
- [ ] implement idempotency replay
- [ ] implement row locking
- [ ] implement ledger writes
- [ ] implement failure handling

## Tests

- [ ] successful transfer
- [ ] insufficient funds
- [ ] idempotent retry
- [ ] ledger correctness
- [ ] concurrency safety

## Documentation

- [ ] update README with setup instructions
- [ ] write PR notes for schema, idempotency, concurrency, and AI usage

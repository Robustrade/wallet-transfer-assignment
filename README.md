# Wallet Transfer Service

Go/PostgreSQL service for wallet-to-wallet transfers with idempotency, row-level locking, stored balances, and double-entry ledger records.

## Features

- Create and fetch wallets.
- Create wallet transfers atomically.
- Replay duplicate transfer requests by `idempotencyKey`.
- Lock source and destination wallets with PostgreSQL `FOR UPDATE`.
- Write one debit and one credit ledger entry for every processed transfer.
- Fetch transfers and wallet ledger entries.

## Run Locally

Start PostgreSQL:

```powershell
docker compose up -d postgres
```

Apply migrations:

```powershell
Get-Content .\migrations\001_init.sql | docker exec -i wallet-transfer-postgres psql -U wallet_user -d wallet_transfer
Get-Content .\migrations\002_create_transfers.sql | docker exec -i wallet-transfer-postgres psql -U wallet_user -d wallet_transfer
Get-Content .\migrations\003_create_ledger_and_idempotency.sql | docker exec -i wallet-transfer-postgres psql -U wallet_user -d wallet_transfer
```

Start the server:

```powershell
go run .\cmd\server
```

The default server listens on:

```text
http://localhost:8080
```

## API Examples

Create wallets:

```powershell
Invoke-RestMethod -Method POST -Uri "http://localhost:8080/wallets" -ContentType "application/json" -Body '{"walletId":"wallet_1","initialBalance":1000}'
Invoke-RestMethod -Method POST -Uri "http://localhost:8080/wallets" -ContentType "application/json" -Body '{"walletId":"wallet_2","initialBalance":500}'
```

Create a transfer:

```powershell
Invoke-RestMethod -Method POST -Uri "http://localhost:8080/transfers" -ContentType "application/json" -Body '{"idempotencyKey":"idem_001","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
```

Replay the same transfer:

```powershell
Invoke-RestMethod -Method POST -Uri "http://localhost:8080/transfers" -ContentType "application/json" -Body '{"idempotencyKey":"idem_001","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
```

Fetch wallet balances:

```powershell
Invoke-RestMethod -Method GET -Uri "http://localhost:8080/wallets/wallet_1"
Invoke-RestMethod -Method GET -Uri "http://localhost:8080/wallets/wallet_2"
```

Fetch a transfer:

```powershell
Invoke-RestMethod -Method GET -Uri "http://localhost:8080/transfers/YOUR_TRANSFER_ID"
```

Fetch wallet ledger:

```powershell
Invoke-RestMethod -Method GET -Uri "http://localhost:8080/wallets/wallet_1/ledger"
```

## Tests

Run the full normal test suite:

```powershell
go test ./...
```

Run PostgreSQL-backed transfer integration tests:

```powershell
docker exec -it wallet-transfer-postgres psql -U wallet_user -d postgres -c "CREATE DATABASE wallet_transfer_test;"
$env:TEST_DATABASE_URL='postgres://wallet_user:wallet_password@localhost:5433/wallet_transfer_test?sslmode=disable'
go test ./internal/service -v
```

The integration tests truncate `wallets`, `transfers`, `ledger_entries`, and `idempotency_records`, so run them only against a test database. If the test database already exists, the `CREATE DATABASE` command can be skipped.

## Design Summary

The service keeps stored wallet balances and updates them in the same transaction as transfer creation and ledger writes. Transfer creation locks both wallet rows in deterministic wallet ID order using `SELECT ... FOR UPDATE`, which prevents concurrent debits from overspending the same wallet.

Idempotency is stored in `idempotency_records`, keyed by `idempotency_key`. A repeated request returns the original transfer without writing another transfer, updating balances, or creating duplicate ledger entries. If concurrent requests race on the same key, the duplicate insert is mapped to a domain error, the transaction is rolled back, and the existing transfer is returned.

Every processed transfer writes exactly two ledger rows: one `DEBIT` for the source wallet and one `CREDIT` for the destination wallet.

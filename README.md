# Wallet Transfer Service

A small Go HTTP service for wallet creation, funding and wallet-to-wallet transfers. It uses SQLite and keeps wallet balances and ledger entries in the same database transactions.

## Run

```bash
go run ./cmd/api
```

The API listens on `:8081` and creates `wallet.db` in the working directory.

## API

Amounts are integer minor currency units (for example, paise or cents).

### Create a wallet

`POST /wallets`

```json
{
  "walletId": "wallet_1",
  "initialBalance": 1000
}
```

`initialBalance` can be zero or greater. New wallets are `ACTIVE` by default. You can optionally set `"status": "INACTIVE"` when creating a wallet.

### Get a wallet

`GET /wallets/{walletId}`

Returns the current balance, status, creation time and last update time.

### Add funds

`POST /wallets/{walletId}/funds`

```json
{
  "amount": 500
}
```

Funding is deliberately simple: the wallet ID is in the URL and the request contains only the amount. Each successful call adds funds once. The balance update and credit ledger entry are committed together. Funding does not use an idempotency key.

### View wallet ledger

`GET /wallets/{walletId}/ledger`

Returns the wallet's opening-balance credit, later funding credits and transfer entries. A missing `transferId` means the entry is a wallet credit rather than part of a transfer.

### Transfer funds

`POST /transfers`

```json
{
  "idempotencyKey": "transfer-request-001",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 250
}
```

A successful transfer debits the source wallet, credits the destination wallet and writes exactly two transfer ledger entries in one transaction. Both wallets must exist and be `ACTIVE`.

The transfer idempotency key is stored with the original request details. Reusing the key with a different payload returns a conflict. If a request fails because the source wallet has insufficient funds, that failure is retryable: add funds to the wallet and submit the original transfer again with the same key and payload. Once the transfer succeeds, later retries return the original transfer.

## Wallet and ledger rules

- Wallet balances are non-negative integer amounts.
- Wallet status is `ACTIVE` or `INACTIVE`; inactive wallets cannot send, receive or add funds.
- Wallet creation with a positive opening balance writes a credit ledger entry.
- Adding funds writes a credit ledger entry.
- Each completed transfer writes one debit and one credit entry.
- Balance changes and their ledger entries are committed atomically.
- `updatedAt` changes whenever a wallet balance changes.

## Database

All table definitions are kept in one place: `internal/platform/schema.sql`. The service applies this schema when it starts. Since this project is not deployed yet, the schema is kept as the initial database definition rather than carrying forward migration code for an older, unused schema.

## Tests

Run:

```bash
go test ./...
```

The repository tests cover transfer balances and ledger entries, duplicate transfer requests, idempotency conflicts, insufficient-funds retries, and concurrent debits. Wallet tests cover creation, opening balances, funding, invalid amounts and inactive wallets.

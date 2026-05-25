# API Contract

Base URL:

```text
http://localhost:8080
```

## Health

```text
GET /health
```

Returns whether the service is running.

Example response:

```json
{
  "status": "ok"
}
```

## Create Wallet

```text
POST /wallets
```

Creates a wallet for testing/demo purposes.

Example request:

```json
{
  "walletId": "wallet_1",
  "initialBalance": 1000
}
```

Example response:

```json
{
  "walletId": "wallet_1",
  "balance": 1000
}
```

## Get Wallet

```text
GET /wallets/{walletId}
```

Returns the wallet and current balance.

Example response:

```json
{
  "walletId": "wallet_1",
  "balance": 900
}
```

## Create Transfer

```text
POST /transfers
```

Moves money from one wallet to another. This is the main assignment endpoint.

Example request:

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

Example success response:

```json
{
  "transferId": "trf_123",
  "status": "PROCESSED",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

Expected behavior:

- repeated requests with the same `idempotencyKey` return the original result
- duplicate requests do not create duplicate ledger entries
- the transfer is atomic
- insufficient funds are handled safely
- concurrent requests cannot overspend a wallet

## Get Transfer

```text
GET /transfers/{transferId}
```

Returns transfer details.

Example response:

```json
{
  "transferId": "trf_123",
  "status": "PROCESSED",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

## List Wallet Ledger Entries

```text
GET /wallets/{walletId}/ledger
```

Returns ledger entries for one wallet.

Example response:

```json
{
  "walletId": "wallet_1",
  "entries": [
    {
      "entryId": "le_1",
      "transferId": "trf_123",
      "type": "DEBIT",
      "amount": 100
    }
  ]
}
```

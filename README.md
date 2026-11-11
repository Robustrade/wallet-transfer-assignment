# Wallet Transfer Service

A wallet transfer API built with Go 1.24, `net/http`, `log/slog`, GORM, and PostgreSQL.

## Run locally

Follow [LOCAL_SETUP.md](./LOCAL_SETUP.md) for sequential **PowerShell** commands:

1. Install Go and PostgreSQL and start the database service.
2. Copy [`.env.example`](./.env.example) to `.env` and set your credentials.
3. Load `.env` into the current terminal; the application does not load it automatically.
4. Run `migrations/setup.sql` through the documented setup command to create the databases, apply the schema, and seed wallets.
5. Start the server in the same terminal:

```powershell
go run ./cmd
```

The default address is `http://127.0.0.1:8080`. Keep `wallet` and `wallet_test` separate. On later launches, reload `.env` and start the server.

## Try a transfer

In a second PowerShell terminal:

```powershell
$request = @{
    Uri = 'http://localhost:8080/transfers'
    Method = 'Post'
    ContentType = 'application/json'
    Body = '{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
}
Invoke-WebRequest @request
```

Transfers update balances and record matching ledger entries atomically. Reusing the same key and payload replays the saved response; a different payload returns `409`. Successful transfers return `201`; insufficient funds returns `422`. Amounts are positive integer minor units.

## Verify

After database setup and loading `.env`, enable CGO and put GCC on PATH as described in [LOCAL_SETUP.md](./LOCAL_SETUP.md#checks), then run:

```powershell
go test -race -count=1 ./...
```

With GNU Make and Bash available, `make` runs formatting, lint, vet, and race tests. Latest verification: **61 tests passed with no races**. Detailed results and profiling findings: [VERIFICATION.md](./VERIFICATION.md).

Database errors, URL encoding, missing configuration, and compiler setup are covered in [troubleshooting](./LOCAL_SETUP.md#troubleshooting). The technology, behavior, and scope summary is in [PR_DESCRIPTION.md](./PR_DESCRIPTION.md). Assignment requirements: [ASSIGNMENT.md](./ASSIGNMENT.md).

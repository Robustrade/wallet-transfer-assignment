# Wallet Transfer Service

Go 1.24, `net/http`, `log/slog`, GORM, and PostgreSQL. The service exposes `POST /transfers` with atomic balance updates, matching ledger entries, and durable idempotency.

Assignment requirements: [ASSIGNMENT.md](./ASSIGNMENT.md). Submit from `solution/<your-name>` to the upstream `main` branch. Reviewer guidance: [evaluation_guide.md](./evaluation_guide.md).

## Local setup (PowerShell)

Run these steps from the repository root. All commands use PowerShell.

1. **Install and check prerequisites.** Install Git, Go 1.24 or a compatible newer version, and PostgreSQL 16 with its command-line tools. Start PostgreSQL using Windows Services if it is stopped. Remember the database username, password, and port selected during installation. For lint/format checks, also install golangci-lint v2. For race tests, install MSYS2 UCRT64 GCC and add its directory as shown under Checks; running the server does not require GCC.

   ```powershell
   # Open PowerShell in your local checkout, then check the tools.
   go version
   # Adjust 16 to your installed PostgreSQL version if needed.
   $env:PATH = "C:\Program Files\PostgreSQL\16\bin;$env:PATH"
   psql --version
   Get-Service *postgres*
   pg_isready -h 127.0.0.1 -p 5432
   go mod download
   ```

   Use the same PostgreSQL port in the checks and your connection URLs. Continue after PostgreSQL reports that it is accepting connections.

2. **Create and edit `.env`.** Copy [`.env.example`](./.env.example) without overwriting an existing local configuration, then replace `USER` and `PASSWORD` in both URLs with your PostgreSQL credentials. Keep the application database `wallet` separate from the integration database `wallet_test`. The test database name must end in `_test`.

   ```powershell
   if (-not (Test-Path .env)) { Copy-Item .env.example .env }
   notepad .env
   ```

   URL-encode special characters inside URL usernames/passwords: `@` becomes `%40`, `#` becomes `%23`, and `%` becomes `%25`. For example, password `hello@123` is written as `hello%40123` in the URL. Keep the URL values quoted as in the example. `sslmode=disable` is for local development only. `.env` is ignored by Git; never commit real credentials.

3. **Load `.env` into this PowerShell session.** Saving the file alone does not configure the process. This block supports the example's plain or quoted `KEY=value` entries. Repeat it after editing `.env` or opening a new terminal.

   ```powershell
   Get-Content .env | ForEach-Object {
       if ($_ -match '^\s*([A-Za-z_][A-Za-z0-9_]*)=(.*)$') {
           $name = $Matches[1]
           $value = $Matches[2].Trim()
           if ($value.Length -ge 2 -and (
               ($value.StartsWith("'") -and $value.EndsWith("'")) -or
               ($value.StartsWith('"') -and $value.EndsWith('"'))
           )) { $value = $value.Substring(1, $value.Length - 2) }
           [Environment]::SetEnvironmentVariable($name, $value, 'Process')
       }
   }
   if (-not $env:DATABASE_URL -or -not $env:TEST_DATABASE_URL) {
       throw 'Set DATABASE_URL and TEST_DATABASE_URL in .env and load it again.'
   }
   ```

4. **Create databases, apply migrations, and seed demo wallets.** Run the setup script below after loading `.env`. The script creates missing databases, applies the schema, and seeds demo wallets. Repeating setup preserves existing balances.

   ```powershell
   $appUri = [Uri]$env:DATABASE_URL
   $testUri = [Uri]$env:TEST_DATABASE_URL
   $appDatabase = [Uri]::UnescapeDataString($appUri.AbsolutePath.TrimStart('/'))
   $testDatabase = [Uri]::UnescapeDataString($testUri.AbsolutePath.TrimStart('/'))
   if (-not $appDatabase -or -not $testDatabase.EndsWith('_test') -or $env:DATABASE_URL -eq $env:TEST_DATABASE_URL) {
       throw 'Configure separate application and *_test database URLs before setup.'
   }
   $adminUrl = $env:DATABASE_URL -replace '^(postgres(?:ql)?://[^/]+/)[^?]*(\?.*)?$', '${1}postgres${2}'
   $testAdminUrl = $env:TEST_DATABASE_URL -replace '^(postgres(?:ql)?://[^/]+/)[^?]*(\?.*)?$', '${1}postgres${2}'
   psql -X -v ON_ERROR_STOP=1 -d $adminUrl -v "application_database=$appDatabase" -v "test_database=$testDatabase" -v "test_admin_url=$testAdminUrl" -f migrations/setup.sql
   if ($LASTEXITCODE -ne 0) { throw 'Database setup failed; resolve the PostgreSQL error before starting the server.' }
   ```

   The role in each URL must have permission to create its database; otherwise an administrator must create it and grant access first. Run `setup.sql` through `psql`.

5. **Confirm setup succeeded.** The setup output should show the test database connection and application schema `version = 2`, `dirty = f`. You can verify both connections separately:

   ```powershell
   psql -X -v ON_ERROR_STOP=1 -d $env:DATABASE_URL -c 'SELECT version, dirty FROM schema_migrations;'
   psql -X -v ON_ERROR_STOP=1 -d $env:TEST_DATABASE_URL -c 'SELECT current_database();'
   ```

   If either fails, fix it before continuing. Later launches only need environment loading and server startup; rerun step 4 when setting up a new database or applying these migrations.

6. **Start the server.** Run in the same terminal where `.env` was loaded.

   ```powershell
   go run ./cmd
   ```

   Expect a `server listening` message at `127.0.0.1:8080` with the example configuration. Keep this terminal open and use a second PowerShell terminal for the request in **Try it** below. Press Ctrl+C to stop the server. For later launches, repeat step 3 and this step; database setup is only needed for a new database.

## Try a transfer

Use a second PowerShell terminal while the server is running:

```powershell
$request = @{
    Uri = 'http://localhost:8080/transfers'
    Method = 'Post'
    ContentType = 'application/json'
    Body = '{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
}
Invoke-WebRequest @request
```

The first successful request returns `201`. Repeating the same key and payload replays the saved response without moving funds again; changing the payload returns `409`. Amounts are positive integer minor units. Insufficient funds returns `422`. Retry pending (`202`) or transient (`503`) responses using the same key and payload with backoff.

## Checks

Load `.env` in this terminal first. Race tests require CGO and an installed MSYS2 UCRT64 GCC; adjust its path if needed. Install golangci-lint v2 for formatting and lint checks.

```powershell
$env:PATH = "C:\msys64\ucrt64\bin;$env:PATH"
$env:CGO_ENABLED = '1'
gcc --version
go mod verify
go build ./...
golangci-lint fmt --diff
golangci-lint run
go vet ./...
New-Item -ItemType Directory -Force profiles | Out-Null
go test -race -count=1 -v -coverprofile=profiles/coverage.out ./...
```

An empty formatting diff is expected. With GNU Make and Bash on PATH, `make` runs formatting, lint, vet, and race tests. Database tests require a separate `*_test` database and permission to create/drop schemas; they skip when `TEST_DATABASE_URL` is absent or `-short` is used.

Latest local verification: **61 tests passed, zero skipped or failed, no race reports**. Setup checks, build, lint, vet, and formatting passed. See [VERIFICATION.md](./VERIFICATION.md) for coverage, profiling findings, and verification limits.

## Troubleshooting

Inspect a database connection error with:

```powershell
psql -X -d $env:DATABASE_URL -c 'SELECT 1;'
```

| Problem | Fix |
| --- | --- |
| `DATABASE_URL is required` | Reload `.env` in the terminal running the server (step 3). |
| Missing database or schema | Run database setup (step 4); the server does not create databases or run migrations. |
| Invalid hostname or authentication failure | Check URL credentials, encode password characters, then reload `.env`. |
| Connection refused | Start PostgreSQL and check the host/port. |
| `psql` or GCC not found | Correct the PostgreSQL/MSYS2 paths shown above. |
| Test database unavailable | Run the connection check with `$env:TEST_DATABASE_URL` and verify its permissions. |
| Port already in use | Stop the other server or change `HTTP_PORT` in `.env` and reload it. |

All environment variables and defaults are listed in [`.env.example`](./.env.example). Restart the server after changing configuration. Wallet management APIs, authentication, background recovery, external payments, and multiple currencies are outside this assignment's scope.

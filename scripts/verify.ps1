$ErrorActionPreference = 'Stop'
if (-not $env:WALLET_TEST_DATABASE_URL) {
    throw 'WALLET_TEST_DATABASE_URL must be set to a disposable PostgreSQL database.'
}
if (-not (Get-Command psql -ErrorAction SilentlyContinue)) {
    throw 'psql must be on PATH. PostgreSQL integration tests cannot be verified without it.'
}
$lines = @(go test ./... -count=1 -v -timeout 8m 2>&1)
$exit = $LASTEXITCODE
$log = $lines -join "`n"
$lines | ForEach-Object { Write-Output $_ }
if ($exit -ne 0) { throw "go test failed with exit code $exit" }
if ($log -match '--- SKIP:') { throw 'A test was skipped: verification is incomplete.' }
$required = @(
    'TestPostgresMigrationContract',
    'TestPostgresTransferService',
    'TestPostgresIdempotencyAndRollback',
    'TestPostgresConcurrentTransfers',
    'TestPostgresTransientTransactionRetries',
    'TestPostgresHTTPTransfers',
    'TestPostgresFinancialBoundaries'
)
foreach ($test in $required) {
    if (-not $log.Contains("--- PASS: $test ") -and -not $log.Contains("--- PASS: $test`n")) {
        throw "Expected PostgreSQL suite not explicitly passed: $test"
    }
}
Write-Host 'VERIFIED: all required PostgreSQL and Go suites passed without skips.' -ForegroundColor Green

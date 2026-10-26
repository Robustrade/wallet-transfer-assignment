$ErrorActionPreference = 'Stop'
$unformatted = @(gofmt -l cmd internal migrations)
if ($LASTEXITCODE -ne 0) { throw 'gofmt could not complete' }
if ($unformatted.Count -ne 0) {
    $unformatted | ForEach-Object { Write-Host $_ }
    throw 'Some Go files are not gofmt-clean'
}
Write-Host 'All Go files are gofmt-clean.' -ForegroundColor Green

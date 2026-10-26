# Final local sign-off and Git submission

## 1. Final verification (Windows PowerShell)

From the extracted final project root, run against a **disposable** PostgreSQL test database:

```powershell
$env:PGPASSWORD = 'YOUR_PASSWORD'
$env:WALLET_TEST_DATABASE_URL = 'postgres://postgres@localhost:5432/wallet_transfer_test?sslmode=disable'
go mod download
go mod verify
.\scripts\check-format.ps1
go vet ./...
.\scripts\verify.ps1
```

The strict verification script must show **no `SKIP` or `FAIL`**, and specifically pass the new `TestPostgresFinancialBoundaries` suite. The final-review `TestPostgresForeignKeyErrorClassification` tests should pass in the full Go run. Optional for confidence: `go test ./internal/service -run '^TestPostgresConcurrentTransfers$' -count=3 -v -timeout 8m` and `go test -race ./... -count=1 -timeout 12m` (the race detector requires a supported Windows C compiler).

Manual demo (development database only): run the server; POST a valid request twice with the same key; compare `transferId`; check balances and ledger SQL. This is supplemental to the integration suite.

## 2. Copy the submission to the official fork

Follow Robustrade's candidate-facing instructions: fork `Robustrade/wallet-transfer-assignment`, then clone **your fork**. Copy the application contents from the extracted final project into that clone, retaining the original `ASSIGNMENT.md`, `.github` files, `LICENSE` and other template files.

```powershell
git clone https://github.com/YOUR_USERNAME/wallet-transfer-assignment.git
cd wallet-transfer-assignment
git switch -c solution/your-name

$solution = 'C:\path\to\robustrade-wallet-transfer-final'
foreach ($folder in @('cmd','internal','migrations','docs','scripts','submission')) {
    Copy-Item "$solution\$folder" -Destination '.' -Recurse -Force
}
foreach ($file in @('README.md','go.mod','go.sum','.gitignore')) {
    Copy-Item "$solution\$file" -Destination '.' -Force
}

git status --short
git add README.md go.mod go.sum .gitignore cmd internal migrations docs scripts submission
git diff --cached --check
git diff --cached --stat
git commit -m "feat: add transactional wallet transfer API and PostgreSQL tests"
git push -u origin solution/your-name
```

If you developed on an existing fork branch already, **do not create a duplicate repository**: use your current fork and copy/update only the relevant files. Review `git status` before committing; do not commit local secrets, database dumps, compiled binaries or test output.

## 3. PR to upstream `main`

Open the fork's GitHub compare/PR flow with **base: `Robustrade/wallet-transfer-assignment` → `main`** and **head: your fork → `solution/your-name`**. Paste and adapt `submission/PR_DESCRIPTION.md` according to the upstream PR template. Update the verification statement only after your actual final run. Do not check the lint/format/test boxes until they pass locally.

**Mandatory AI disclosure:** The assignment requires the complete authentic AI-session transcript in the repository **or** emailed with your submission. If genuinely unavailable, provide **all prompts**. The final project contains only the disclosure summary, not the transcript itself. Deliver it and replace the explicit placeholder in the PR draft. Check transcript contents for credentials or unrelated private material before publishing. Do not falsely claim the transcript has been attached.

Check that the PR includes the full application rather than the ZIP binary; ensure no conflicting generated scaffolding or unrelated edits are committed. The template's CI may be configured for a custom runner, so an upstream CI badge is not a substitute for the local verification script.

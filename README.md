# Wallet Transfer Assignment Repository

This repository is a reusable coding assignment template for evaluating backend engineers on wallet transfers, idempotency, concurrency control, and double-entry ledger design.

## Included

- `ASSIGNMENT.md` - candidate-facing prompt
- `.github/pull_request_template.md` - required PR structure
- `.github/workflows/ci.yml` - lint, format, test placeholder workflow
- `.github/workflows/sonarqube.yml` - SonarQube pull request analysis
- `.github/copilot-instructions.md` - repository-level Copilot review guidance
- `evaluation_guide.md` - reviewer rubric
- `branch-protection-checklist.md` - GitHub setup checklist

## Intended use

1. Mark this repository as a GitHub template repository.
2. Create one private repository per candidate from the template.
3. Add the candidate as a collaborator.
4. Ask them to submit via a pull request into `main`.
5. Enable required checks, SonarQube, and Copilot review in GitHub.

## Notes

- Copilot automatic pull request review is configured in GitHub repository or organization settings, not purely through files in the repo.
- The `copilot-instructions.md` file included here provides repository-specific review guidance once Copilot review is enabled.
- The CI workflow is language-agnostic by default and expects you to set the `LINT_CMD`, `FORMAT_CHECK_CMD`, and `TEST_CMD` repository variables or replace the commands directly.

## How to Submit Assignment

1. **Fork this repository** to your own GitHub account.
2. Complete the assignment described in [`ASSIGNMENT.md`](./ASSIGNMENT.md).
3. **Raise a Pull Request** back to this repository (`main` branch) with your full solution.

Your PR branch should be named: `solution/<your-name>` (e.g., `solution/jane-doe`).


--------------------------------------------------------
## Summary
Implemented a concurrent, transactional wallet-to-wallet transfer service in Go backed by PostgreSQL. The service supports atomic balance movement, double-entry ledger bookkeeping, deterministic deadlock prevention, and exact-once execution semantics via lease-managed idempotency records. It follows a clean layered architecture (handler -> service -> repository -> model), backed by an automated integration test suite and an end-to-end shell test suite.

## AI disclosure
1. What tool you used (Cursor, Claude Code, Antigravity etc.)
   * Primary Tool: Gemini (Google AI) web interface and GitHub Copilot.
2. How you generally use the tool for your work.
   * For my official work I use Claude and Copilot for coding, discussing industry standards for a given design solution, writing test scripts, and performing code reviews.
3. A transcript of your entire session with your AI tool of choice. You can add this to the repo or email it to us with your submission. If for some reason, this is not possible, give us all the prompts that you used with the AI.
   * Interactive Code Review & Pair Programming: Used as an engineering partner to validate architectural decisions, evaluate trade-offs of storing amounts in minor units (BIGINT) vs. floats to prevent precision/concurrency issues, and refine SQL table constraints.
   * Framework scaffolding: Structured a layered Gin skeleton with native PostgreSQL drivers (pgx).
   * Test automation: Developed behavioral integration tests (tests/transfer_test.go) and shell verification scripts (scripts/test_e2e.sh).
   * Environment & edge-case debugging: Resolved Windows Git Bash newline artifacts (CRLF to LF), fixed validation tags for wallet identifiers (underscores only), and added mathematical overflow guards on currency conversions.
   * Code review: Addressed automated code review feedback regarding transactional row checks, idempotency persistence guarantees, and test resilience.

## Schema Design
Defined in: migrations/1_init_schema.setup.sql (for full details)

### Tables
* wallets: Stores account state (id, balance, status, created_at, updated_at). Balances are stored as 64-bit integers (BIGINT) representing minor units (cents) to eliminate floating-point drift.
* transfers: Stores money movement records (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at).
* ledger_entries: Immutable general ledger capturing every debit and credit event (id, wallet_id, transfer_id, type, amount, balance_before, balance_after, created_at).
* idempotency_records: Manages execution locks and cached responses (key, request_hash, status, response_code, response_body, created_at, updated_at).

### Constraints & Invariants
* Non-Negative Balance: CHECK (balance >= 0) on wallets guarantees that balance overdrafts are impossible at the database engine level.
* Positive Transfer Amounts: CHECK (amount > 0) on transfers and ledger_entries.
* Double-Entry Symmetry: Unique constraint uq_transfer_wallet_type (transfer_id, wallet_id, type) on ledger_entries ensures a transfer cannot double-debit or double-credit the same wallet.
* Referential Integrity: Foreign keys from transfers and ledger_entries to wallets(id).

### Indexes
* wallets(id): Primary key index for O(1) row lookups and locks.
* ledger_entries(wallet_id, created_at DESC): Composite index optimized for rapid wallet statement generation and audit history scans.
* idempotency_records(key): Primary key index for instant lease checks and replay lookups.

## Idempotency Strategy
Explain how duplicate requests are handled safely.
* Payload Fingerprinting: On every transfer request, the service normalizes the incoming payload, parses amounts to minor units, and computes a deterministic SHA-256 hash stored alongside the user-supplied key.
* Atomic Lease Lock: Before moving funds, the system attempts to insert or update a record in idempotency_records with status = 'STARTED' and timestamp updated_at.
* First Request Execution: Upon acquiring the lease, the transfer executes atomically within a database transaction. Before commit, the idempotency record is verified and transitioned to status = 'COMPLETED' with the HTTP status code and response payload cached.
* Safe Replay Without Re-Execution: If an incoming request matches an existing COMPLETED key and has an identical SHA-256 payload hash, business logic and balance mutations are bypassed. The cached response body and HTTP status code are served immediately.
* Tamper Detection: If an existing key is reused with altered parameters (e.g., changed amount or recipient), the SHA-256 hashes mismatch. The system halts immediately with HTTP 400 Bad Request ("idempotency key already used with different payload").
* In-Flight Conflict Suppression: If a duplicate request arrives while an operation is actively processing (status = 'STARTED' and updated_at + TTL has not expired), the system rejects concurrent mutation with HTTP 409 Conflict.

## Concurrency Strategy
* Pessimistic Row-Level Locking (FOR UPDATE): Balances are modified within a PostgreSQL transaction using `SELECT balance, status FROM wallets WHERE id IN (1, 2) ORDER BY id FOR UPDATE`. This ensures only one transfer can read and mutate a wallet's balance at any given moment; subsequent parallel attempts must wait until the active transaction commits or rolls back.
* Deterministic Lock Ordering: To eliminate SQL deadlocks when opposite transfers occur concurrently (e.g., Alice -> Bob while Bob -> Alice), row locks are always acquired in strict lexicographical order:
  firstID, secondID := fromID, toID
  if firstID > secondID {
      firstID, secondID = toID, fromID
  }
  // Lock firstID, then lock secondID
  This eliminates circular wait states, mathematically guaranteeing zero deadlocks across high-concurrency transfers.
* Double-Spending Prevention: Database row-level locks serialize balance reads and mutations for any given wallet. Combined with the engine-level CHECK (balance >= 0) constraint, no balance can be debited beyond its available funds.
* High-Concurrency Verification: Verified via TestTransfer_HighConcurrencyNoDoubleSpend, where 10 concurrent goroutines attempt to spend from a shared balance simultaneously (100 initial balance, 10 transfers of 15). Exactly 6 succeed (90 spent) and 4 fail with insufficient balance, preserving the exact final database invariants.

Explain how you prevent race conditions and double spending.
Pessimistic Row-Level Locking (FOR UPDATE): Before checking balances or applying debits/credits, the transaction locks both participating wallet records via SELECT balance, status FROM wallets WHERE id = 1 FOR UPDATE. This ensures only one transfer can read and mutate a wallet's balance at any given moment; subsequent parallel attempts must wait until the active transaction commits or rolls back.
Deterministic Lexicographical Lock Ordering: To prevent deadlocks when opposing transfers occur concurrently (e.g., Alice transferring to Bob while Bob transfers to Alice), wallets are locked in strict alphabetical order

## Environment Configuration
Set the following environment variables before starting the server (Note env will change as per your system):
PORT=8080
ENVIRONMENT=development
DATABASE_URL=postgresql://postgres:postgres@localhost:5432/walletDB?sslmode=disable
IDEMPOTENCY_LOCK_TTL_SECONDS=30
IDEMPOTENCY_RETENTION_SECONDS=86400
ALLOWED_ORIGINS=http://localhost:3000

## How to Run
1. Ensure PostgreSQL is running and initialize the database schema:
   createdb walletDB
   psql -d walletDB -f migrations/1_init_schema.setup.sql
2. Start the API server:
   go run cmd/server/main.go

## How to Test
* Integration Tests: Runs the suite covering transfer execution, idempotency replays, payload tamper detection, insufficient balances, and concurrent race conditions:
  export TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5432/walletDB_test?sslmode=disable"
  go test -v ./tests/...
* End-to-End Suite: Runs client-side verification with formatted JSON assertions against a live server:
  chmod +x scripts/test_e2e.sh
  ./scripts/test_e2e.sh

## Tradeoffs / Assumptions
* Stored Balances vs. Pure Event-Sourced Ledger: Rather than recalculating balances by aggregating all historic ledger rows on every read (O(N) query cost), we store an explicit balance column on wallets and mutate it atomically alongside ledger_entries. This provides O(1) balance reads and row locks while preserving complete double-entry auditability.
* Minor-Unit Arithmetic & Overflow Boundary: All monetary values are parsed into 64-bit signed integers (int64 cents) to avoid IEEE-754 floating-point inaccuracies. Inputs are capped at (math.MaxInt64 - 99) / 100 to prevent integer multiplication overflow.
* Identifier Formatting: Wallet IDs enforce an alphanumeric and underscore character set (^[a-zA-Z0-9_]+) via custom Gin binding validators; hyphens are disallowed.
* Local Test Infrastructure: Tests run against a live PostgreSQL instance rather than an in-memory SQL mock to validate row-level locking semantics (FOR UPDATE), isolation boundaries, and check constraints directly.

## API Usage & cURL Examples
Before using cURL examples, ensure the database schema has been initialized via migrations/1_init_schema.setup.sql.

### 1. Create Wallet 1 (100.00)
curl -s -X POST "http://localhost:8080/api/v1/wallets" \
  -H "Content-Type: application/json" \
  -d '{
    "id": "Vishal",
    "initialBalance": "100.00",
    "status": "ACTIVE"
  }'

### 2. Create Wallet 2 (20.00)
curl -s -X POST "http://localhost:8080/api/v1/wallets" \
  -H "Content-Type: application/json" \
  -d '{
    "id": "Sri",
    "initialBalance": "20.00",
    "status": "ACTIVE"
  }'

### 3. Execute Transfer (25.00 from Vishal to Sri)
curl -s -X POST "http://localhost:8080/api/v1/transfers" \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "tx_key_001",
    "fromWalletId": "Vishal",
    "toWalletId": "Sri",
    "amount": "25.00"
  }'

### 4. Idempotent Replay (Identical Key & Payload -> Cached Response)
curl -s -X POST "http://localhost:8080/api/v1/transfers" \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "tx_key_001",
    "fromWalletId": "Vishal",
    "toWalletId": "Sri",
    "amount": "25.00"
  }'

### 5. Tampered Payload with Same Key (Fails with HTTP 400)
curl -i -s -X POST "http://localhost:8080/api/v1/transfers" \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "tx_key_001",
    "fromWalletId": "Vishal",
    "toWalletId": "Sri",
    "amount": "50.00"
  }'

### 6. Fetch Wallet 1 Statement (75.00 balance + DEBIT entry)
curl -s -X GET "http://localhost:8080/api/v1/wallets/Vishal/statement"

### 7. Fetch Wallet 2 Statement (45.00 balance + CREDIT entry)
curl -s -X GET "http://localhost:8080/api/v1/wallets/Sri/statement"

## Checklist
- [x] Tests pass
- [x] Lint passes
- [x] Format check passes
- [x] README or notes updated
- [x] PR description explains schema, idempotency, and concurrency
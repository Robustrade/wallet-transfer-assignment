# Wallet transfer service

Build a small, reliable Go service for wallet-to-wallet transfers. Keep balances
and ledger records correct under duplicate requests, retries, network failures,
partial execution, and simultaneous transfers. Prioritize correctness and clarity
within the assignment's expected 3–5 hours of effort.

## Requirements

- [Assignment brief](ASSIGNMENT.md)
- [Evaluation guide](evaluation_guide.md)
- [Submission instructions](README.md)
- [PR template](.github/pull_request_template.md)

Read the relevant original requirements before substantial work. The relative
links also work when the repository is checked out elsewhere.

## Required behavior

Implement `POST /transfers` with this request shape:

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

1. **Idempotency:** Persist the key and original result. Repeated requests with
   the same key must return the original result without repeating money movement
   or ledger entries, including after a lost response or process restart.
2. **Atomicity:** Commit the debit, credit, ledger entries, and successful
   transfer status together. Failures must never leave a partial transfer.
3. **Double-entry ledger:** Each processed transfer produces exactly two entries
   linked to its transfer ID: a source debit and an equal destination credit.
4. **Balances and concurrency:** Maintain correct balances and prevent double
   spending when requests execute simultaneously. If a wallet has 100 units, two
   concurrent transfers of 80 units cannot both succeed. Choose and justify the
   database transaction, locking, or isolation strategy.
5. **States:** Support `PENDING -> PROCESSED` and `PENDING -> FAILED`, with guarded
   transitions that remain safe under retries and duplicate requests.

For example, transferring 100 units from balances of 500 and 200 must result in
400 and 300. Replaying the request must leave those balances unchanged.

## Persistence and structure

Work in the `wallet-service` Go module and build on its existing scaffold.
Keep HTTP handlers thin, transfer orchestration in services, persistence in
repositories, and entities and state rules in domain models.

Prefer PostgreSQL; SQLite is also acceptable. Suggested tables are `wallets`,
`transfers`, `ledger_entries`, and `idempotency_records`. Use database constraints
and appropriate indexes to protect integrity and deduplication. Either derive
balances from the ledger or store balances updated atomically with transfers.

## Development and verification

Before nontrivial implementation, document the API and error contract, side
effects, failure modes, idempotency and retry behavior, consistency guarantees,
observability expectations, and testing strategy. Make assumptions explicit,
including amount units, insufficient funds, missing wallets, and reuse of a key
with a different request payload.

For transfer behavior, write a failing behavioral test, implement the smallest
correct solution, then refactor while keeping tests passing. Required coverage
includes successful transfers, duplicate requests, balanced ledger entries, and
failure scenarios. Concurrency safety is required; tests demonstrating it are
encouraged. Run formatting, vet, build, and relevant tests from the Go module;
use `go test -race ./...` when exercising concurrent Go code.

Balance and history APIs, extra logging, metrics, and additional retry workflows
are optional. Keep the work focused on the requested assignment behavior.

## Documentation and submission

Keep the README's run and test instructions current. Append brief numbered notes
to [AI_USAGE.md](wallet-service/AI_USAGE.md) for meaningful LLM-assisted work. For
submission, disclose the AI tool and how it was used, and provide the full session
transcript or all prompts if the transcript is unavailable.

At submission, use a PR on `solution/<your-name>` into `main`, following the
original PR template. Explain schema design, constraints, idempotency,
concurrency, validation, assumptions, and tradeoffs in language the candidate can
defend in a follow-up discussion.

# AI usage

**Tool:** OpenAI Codex.

**How it was used:** To scaffold the Go project, organize the assignment files,
document the project requirements as default Codex instructions, plan
individually mergeable implementation steps, and draft PostgreSQL migrations
with explanations of the schema and its constraints.
Also used to finish the runnable baseline, CI checks, and concise run instructions.
Implemented and verified PostgreSQL setup, domain rules, and repository operations.
Also implemented and tested the transactional transfer service.
Exposed the HTTP endpoint with focused tests and request/outcome logging.
Prepared the submission docs and original prompt record.

## Session notes

1. Asked the LLM to scaffold a Go project in a new directory.
2. Asked the LLM to organize the assignment documents.
3. Asked the LLM to create a simple Codex skill with links to the requirements.
4. Asked the LLM to use Codex's default project instructions instead of a named skill.
5. Asked the LLM to plan step-by-step implementation with a mergeable deliverable for each step.
6. Asked the LLM to create a PostgreSQL wallets-only migration with comments explaining the database types, without running it. Added forward and rollback SQL, documented currency and balance assumptions, and prepared a patch for the fork.
7. Asked the LLM to move all migration comments above the table definition and explain the type choices with readable currency conversion examples.
8. Asked the LLM to add the supplied transfers schema to the same migration file. Added comments above the table, updated the rollback order and documentation, and left the migration unexecuted.
9. Asked the LLM to store currency on transfers and enforce matching currencies through composite foreign keys to wallets. Updated the migration, explanatory comments, documentation, and fork patch without executing SQL.
10. Asked the LLM to add ledger entries and idempotency records to the same migration and complete its supporting SQL. Added the tables, wallet lookup indexes, dependency-ordered rollback, and documentation; left database execution deferred.
11. Reviewed Step 1 after drafting the schema; added concise run instructions and Go CI, and verified formatting, vet, build, race-enabled checks (with a fresh Go cache), health responses, and signal shutdown. The scaffold has no automated tests yet.
12. Removed duplicate documents, kept assignment originals at the repository root, and cleaned up documentation references.
13. Wrote the Step 2 API/transaction contract with error, replay, locking, recovery, and test rules plus three Mermaid diagrams; checked it against the schema and PostgreSQL documentation.
14. Completed Step 3 with Compose, versioned migrations, safe seeds, configuration, domain rules, and seven transaction-bound repository operations. Added PostgreSQL/CI tests for constraints, rollback, replay, locking, and cancellation; verified Go checks, race tests, startup errors, health, and shutdown. Raised the Go minimum to 1.25.11 for dependencies.
15. Completed Step 4 with one transfer service using the existing repository API and a shared database fixture. Verified success, replay/conflicts, saved failures, competing debits, rollback/retry, cancellation, and simulated lost commit acknowledgments; formatting, vet, build, and race tests pass on Go 1.25.11.
16. Completed Step 5 with strict JSON decoding, HTTP error mapping, exact replay responses, request IDs, and outcome logs. Added two focused handler tests and one PostgreSQL HTTP scenario; extended existing log assertions. Go checks/race tests pass on 1.25.11; verified curl examples and shutdown against a disposable database.
17. Skipped Step 6 at the author's request and completed Step 7: corrected stale docs, exported 72 original prompts, and prepared the upstream PR body/creation plan. Verified documented setup, API examples, seed preservation, shutdown, and all Go/PostgreSQL checks in a clean temporary copy on Go 1.25.11. No staging, commits, push, or PR creation performed.

## Original prompts

[All 72 user prompts](docs/ai-prompts.md) from the three project sessions are
included through the PR-preparation request; only trailing whitespace is trimmed.
This uses the assignment's all-prompts alternative to a full transcript. Add any
later prompts before submission.

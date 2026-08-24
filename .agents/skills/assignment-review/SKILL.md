---
name: assignment-review
description: Self-review the current diff against evaluation_guide.md and .github/copilot-instructions.md's rubric before opening the PR into Robustrade/wallet-transfer-assignment. Use before opening the PR, or after a significant chunk of implementation, to catch what the automated Copilot reviewer would flag first.
---

# Assignment Review

Runs this repo's own grading rubric against the diff, before the PR does. Report findings,
then ask before editing — this is a review pass, not an auto-fixer.

## Steps

1. Read `evaluation_guide.md` and `.github/copilot-instructions.md` in full — they are the actual
   rubric, not a paraphrase of it.
2. Get the diff: `git diff main...HEAD` (or the relevant base branch).
3. Walk the diff against each rubric section, citing file:line for every finding:
   - **Schema** — sensible tables, PK/FK, a real uniqueness constraint on the idempotency key
     (not just an index), constraints that make invalid states unrepresentable, useful indexes.
     Can a ledger row exist without a transfer? Can a duplicate transfer happen by accident?
   - **Transactions & locking** — explicit transaction boundaries around the debit + credit +
     ledger-entry + idempotency-record write (all-or-nothing, not split). Any read-then-write
     race (balance checked in one statement, debited in another, with no lock or version check
     between them)? Insufficient-funds handling explicit and tested?
   - **Idempotency** — durable storage of the key, safe replay (same response on retry, zero
     duplicate side effects), and specifically: does the idempotency check happen in the *same*
     transaction as the write it's guarding, or could two concurrent identical requests both
     pass the check before either commits?
   - **Layering** — run `just lint` (covers `lint-imports` against `.importlinter`'s
     handler->service->repository->domain contract) and read the diff for business logic in a
     handler, or workflow decisions (e.g. insufficient-funds branching) in a repository.
   - **Tests** — do they assert on behavior (response bodies, DB state) rather than internals?
     Is there a test for: duplicate/replayed request, concurrent same-wallet debits, a failed
     transfer, ledger balance after a transfer? Would any of these tests actually fail if the
     bug it's meant to catch were reintroduced — or does it pass either way?
   - **Development practices** — are commits topical and conventional-commit-prefixed? Is
     `ARCHITECTURE.md` updated for any new top-level component in the same diff?
4. Report findings grouped by rubric section, each with the specific concern and file:line
   evidence — not a generic checklist restated. Ask before making any edit.

## When to use

Before opening the PR, and after any substantial chunk of implementation (e.g. after the
concurrency/locking logic lands, or after idempotency handling lands) — catching a layering leak
or an unsynchronized idempotency check early is cheaper than after the whole service is wired up.

## When NOT to use

Mid-implementation on unfinished, known-incomplete code — wait until a feature is functionally
complete enough that "is this correct" is a meaningful question, not "is this done yet."

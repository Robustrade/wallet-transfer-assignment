---
name: new-adr
description: Scaffold a new Architecture Decision Record from the MADR template with the next sequential number. Use when about to make a non-trivial design decision (schema, idempotency mechanism, locking strategy, framework choice) that ASSIGNMENT.md's Documentation-First Workflow requires be written down before implementation.
---

# New ADR

Scaffolds a new decision record — never invents the decision itself.

## Steps

1. List `docs/decisions/adrs/adr-*.md`, find the highest `NNN`, compute the next number
   (zero-padded to match existing width, e.g. `0001` -> `0002`).
2. Ask the user (if not already clear from conversation) for: the short imperative title, and
   which options are genuinely being considered — don't guess at a decision that hasn't been
   made yet.
3. Copy `adr-000-madr-template.md` to `adr-<NNN>-<slug>.md` (slug = lowercase, hyphenated title).
4. Fill in the frontmatter (`status: proposed`, `owner`, today's date) and the title/deciders
   line. Leave every content section (`Context`, `Decision drivers`, `Considered options`,
   `Decision`, `Consequences`) for the user to fill in collaboratively — draft them together in
   the conversation, don't silently invent rationale.
5. Delete the template's trailing "To use this template" note from the new file.

## When to use

Before implementing a non-trivial design decision — matches `ASSIGNMENT.md`'s required order:
inspect spec -> update spec/design note -> implement -> test.

## When NOT to use

For a decision that's actually trivial (variable naming, which test framework helper to use) —
ADRs are for choices a future reader would need justified, not every code-level judgment call.

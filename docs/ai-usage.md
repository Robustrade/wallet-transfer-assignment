# AI Usage Disclosure

Required by `ASSIGNMENT.md`'s AI usage section. This note covers the tool
used, how it was used, and the prompts that drove the work.

## 1. Tool

**Claude Code** (Anthropic's CLI coding agent), used in two passes:

- **Claude Opus 5** for the architecture and planning pass: reading the
  assignment and the repository's existing conventions (CI pins, PR
  template, evaluation rubric, a sibling project's Go/Postgres/Makefile
  conventions), then producing a complete build specification —
  [`prompt.md`](../prompt.md) — covering the schema, the locking strategy
  and its justification, the idempotency mechanism, the HTTP contract, the
  test plan, the commit sequence, and an explicit list of things *not* to
  build.
- **Claude Sonnet 5** for implementation against that specification: writing
  the Go code, the SQL migrations, the tests, running them against a real
  Postgres instance, fixing the two real bugs the tests and a manual
  end-to-end walkthrough surfaced (see below), and getting `golangci-lint`
  actually clean rather than assuming the config was correct.

## 2. How the tool was generally used

The workflow was documentation-first, matching what `ASSIGNMENT.md` itself
asks for: design in prose and commit it (`docs/design.md`, `docs/api.md`)
*before* writing implementation code, then implement layer by layer
(domain → postgres → service → HTTP → integration tests → CI → docs),
verifying each layer builds and its tests pass before moving to the next,
and committing each layer as its own reviewable, buildable commit.

The specific engineering decisions were made and justified by a human
working with the model, not delegated wholesale:

- **`SELECT ... FOR NO KEY UPDATE` instead of `FOR UPDATE`** for locking
  wallets, specifically to avoid a deadlock mode that ordered locking alone
  doesn't fix (the implicit `FOR KEY SHARE` a transfer insert takes on both
  wallets for its foreign keys). See `docs/design.md` §7.
- **Insufficient funds is a committed `FAILED` transfer, not a returned
  error** — chosen so a retry with the same idempotency key gets a stable
  answer forever, rather than a result that depends on when the retry
  happens. See `docs/design.md` §8.
- **No separate `idempotency_records` table**, even though `ASSIGNMENT.md`
  suggests one as an option — the unique constraint on `transfers` already
  is the dedup mechanism for this service's one mutating endpoint, and a
  second table would just be state that could disagree with it. See
  `docs/design.md` §6 and §12 for the alternatives that were considered and
  why this one won.

The model was also used to find and fix real bugs, not just generate code
that looked plausible: a manual `curl` walkthrough of the documented API
surfaced that `createdAt` was silently coming back as the Go zero-time
value (fixed by generating the timestamp once in the service layer and
using it for both the row and the response, the same pattern already used
for `settledAt`); a deliberately adversarial test case
(`{"amount":"100"}`) surfaced that `encoding/json.Number` accepts a JSON
*string* into a numeric field just as readily as a JSON number, so it was
replaced with a custom `strictInt64` type; and running `golangci-lint`
under the exact Go version CI pins (rather than trusting the config file
was correct) surfaced a cyclomatic-complexity violation and two `gosec`
findings, all fixed rather than suppressed except where suppression was the
actually-correct answer (backoff jitter is not a security-sensitive value).

Two follow-up requests after the first working version was reviewed were
handled the same way — implement, don't just agree: asked to simplify and
remove overengineering, the model audited the codebase and found one real
offender (a full keyset-pagination system on an endpoint `ASSIGNMENT.md`
lists as optional) rather than making cosmetic cuts to code that was
already load-bearing for what the assignment evaluates. Asked to add an
ORM — a direct reversal of the original plan's explicit anti-goal — the
model flagged the one real risk once (that this service's correctness
rests on exact lock-clause and guarded-update SQL, which most ORM query
builders don't let you write precisely) via a clarifying question about
*which* ORM, then implemented GORM as requested with those specific
statements kept as raw SQL through GORM's own escape hatches
(`clause.Locking`, `.Raw()`, `clause.OnConflict`) rather than left to its
query builder. See `docs/design.md` §13 for the full accounting of what
changed and what deliberately didn't.

## 3. Prompts

The complete specification the implementation was built against is
committed in this repository as [`prompt.md`](../prompt.md) — that file
*is* the detailed instruction set, and is itself a work product of the
planning pass described above, reviewed and accepted before implementation
began.

The human-authored prompts that drove the two Claude Code sessions,
verbatim:

**Planning session (Claude Opus 5):**

> As a senior engineer and architect, plan task @ASSIGNMENT.md . After you plan, build a very detailed prompt.md to be passed on for sonnet5 so it can build it

**Implementation session (Claude Sonnet 5):** the session was switched to
the Sonnet 5 model and pointed at the produced specification with the
file reference `@prompt.md`; the model treated that as the instruction to
execute the specification (`prompt.md` §0 states explicitly that it is a
complete build spec addressed to "Claude Sonnet 5 (implementer)" and that
implementation should proceed without re-confirming scope). No further
manual prompt text was required to drive the full implementation, testing,
debugging, and documentation pass — the specification carried that
detail.

A full session transcript, if the reviewer would like one, is available on
request from the repository owner — the terminal scrollback was not
exported into this repository by default to avoid committing an
unreviewed, very large text file. *(Owner: attach a transcript export here,
or your contact details, before submitting if you want to include one.)*

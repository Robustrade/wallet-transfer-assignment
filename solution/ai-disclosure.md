# AI Disclosure

## 1. Tool used

Claude Code (Anthropic), running as the Sonnet 5 model, via the CLI/IDE
integration.

## 2. How it was generally used

Interactively, one request at a time, with every non-trivial output
reviewed and frequently pushed back on before moving forward — not used
to blindly generate a finished solution in one shot.

1. Asked it to clone and review the upstream template repository for before building on top of it.
2. Asked it to read `ASSIGNMENT.md` and scaffold a layered project (handler -> service -> repository -> domain) in Go with PostgreSQL.
3. Reviewed the scaffold and asked follow-up questions about specific structural choices (DTO placement, a generic `utils/` package, splitting DB config out).
4. Asked it to move the scaffold into a dedicated `solution/` subfolder separate from the template's own docs.
5. Asked it to add graceful shutdown and request-logging middleware. Separately, asked it to review the repository implementations once real SQL started being written (partly directly in the IDE), which surfaced real bugs: a wrong table name, a missing column, timestamp type mismatches, and a row lock that didn't actually hold across a later write.
6. Fixing the row-lock bug required threading a transaction through the repositories
7. Asked for a cleanup pass (removing anything unnecessary), moving `pgerr.go` into a narrowly-scoped `internal/utils` package, adding observability (structured `log/slog` logging throughout), writing real automated tests (unit tests against fakes, plus Postgres integration tests including a concurrency test), and writing a proper `solution/README.md` (architecture, API reference, build/run/test/deploy, pros/cons, known limitations).
8. Asked for a `scripts/seed.sql` script (re-runnable wallet seed data) plus a `make seed` target, since there's no wallet-creation API.
9. Asked it to check the actual `evaluation_guide.md` rubric against the real implementation. It gave an honest per-category assessment, including flagging the gaps that still existed at that point.

## 3. Prompts used this session

Grouped to line up with the numbered points in section 2 above — one
group per point, in the same order — plus one extra group at the end for
git/process prompts that section 2 doesn't narrate step-by-step. Nothing
is omitted or reworded from what was actually typed.

**1. Clone and review the upstream template repository**

- "Clone this repo and make sure that nothing is scam or wrong packages
  or such — https://github.com/heisenberglit/wallet-transfer-assignment"

**2. Read ASSIGNMENT.md and scaffold a layered project**

- "Create template structure for the same go through assignment.md and
  first create folder structure and add readme and all"

**3. Follow-up questions on structural choices**

- "Should we move the param request and response type into a domain
  folder? and have utils folder with utility and also have a different
  db file to have connection and pooling and all those config"

**4. Move the scaffold into `solution/`**

- "I think the files we changed and all are merging with assignment doc
  and all. Create a folder and move the changes inside that and
  simplify the structure then like move migrations out from db to that
  folder"

**5. Graceful shutdown, logging middleware, and the bug-review pass**

- "Add graceful shutdown and request/response middleware logger"
- "Can you check implementation in wallet_repository"

**6. Threading a transaction through the repositories**

- "Anything that you think we are missing currently or over doing
  anything?"
- "Let's simplify it and remove the pool tuning env vars and use them
  directly in code"

**7. Cleanup, observability, tests, and the README**

- "Remove anything unnecessary and move pgerr.go to utils and
  also add observability wherever needed and unit and integration test cases. Also add
  README.md for this which contains details of endpoints and how to
  build, run and deploy and add architecure diagram and known limitations."

**8. Seed script**

- "Create seed data script as well so that we can run that for
  testing"

**9. Evaluation-rubric check**

- "Check the evaludation criteria file and check we have everything
  implmeneted and ready"

A full transcript of the session is available on request if needed
beyond this summary.

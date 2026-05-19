# AI usage disclosure

## Tool used

**Cursor** (AI-assisted IDE / agent)

## How I generally use the tool

- **Scaffolding** — project layout, migrations, repository interfaces, HTTP routing
- **Implementation help** — PostgreSQL queries, service workflow decomposition, unit test structure
- **Design alignment** — schema, pessimistic locking, and idempotency per `implementation-details.md`
- **Debugging** — Docker port 5432/5433 conflict, `golang-migrate`, Go `GOTOOLCHAIN` on Windows
- **Documentation** — README (local run guide)

I review all AI-generated code and can explain it in an interview.

---

## Prompts used (full list)

1. > add postgres database configution in this repo, and create a dababase migration from schema mentioned in @implementation-details.md

2. > write me a Readme file for this project

3. > start implementing using @implementation-details.md and @ASSIGNMENT.md , only implement structure, keep business logic implementation for me, i would implement that, thanks

4. > how would you implement this?  
   > (referring to `TransferRepository.Create`)

5. > write all required queries in repository/postgres

6. > can add functions for inside working of createtransfer in @internal/service/transfer.go

7. > can write unit tests coverage mentioned in @ASSIGNMENT.md file, thanks

8. > `make test` — Go toolchain download failures (`go1.24` / `go1.22` / `go1.23` not available on Windows)

9. > how can i test end to end?


---

## Transcript

A full Cursor agent session export can be shared separately if required. This file lists all user prompts; the agent’s step-by-step tool output is available in the Cursor chat history for this project.

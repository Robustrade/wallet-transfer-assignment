# AI usage and available user prompts

Tool: **OpenAI Codex**, used in the shared repository workspace.

I used Codex to refine requirements, plan incremental work, generate tests, review code, and assist with verification. I reviewed its suggestions and retained responsibility for implementation and design decisions.

- Created the initial folder structure and configuration files, including `.golangci.yml`, `.env`, and `.env.example`, and documented the implementation plan in `README.md`.
- Drafted the requirements, system design, and assumptions, then asked Codex to refine them into an incremental implementation plan.
- Asked Codex to propose a database schema and migration strategy, which I reviewed before implementation.
- Implemented tasks incrementally and used Codex to generate supporting test cases.
- Reviewed the generated tests and removed or consolidated cases I considered repetitive or outside the assessment’s scope, including some migration, configuration, and crash scenarios.
- Asked Codex to review the implementation and suggest improvements. I evaluated those recommendations and rejected or revised several:
  - **Configuration and database lifecycle:** chose process-wide singleton initialization.
  - **Connection pooling:** retained the library’s default pool management instead of introducing custom pool configuration.
  - **Initialization failures:** rejected a plain `sync.Once` approach that could prevent retry after failed database initialization. Configuration caches its validated result and error; database initialization caches only a successful connection.
  - **Error handling:** used custom application errors with explicit classification and HTTP mapping.
  - **Transfer identifiers:** chose database-generated UUIDs instead of application-generated identifiers.
  - Reviewed and adjusted other implementation details where the proposed approach did not match the intended design.
- Completed verification with Codex’s assistance, including Makefile checks, race-enabled tests, PostgreSQL integration tests, and CPU, memory, goroutine, mutex, and block profiling.

* Planned and implemented the tests myself, then ran the Makefile checks, race-enabled tests, PostgreSQL integration tests, and profiling utilities to verify the solution.
* Used Codex to refine the wording and structure of VERIFICATION.md, which records the verification commands, results, findings, and limitations.

Commands, results, findings, and limitations are documented in [`VERIFICATION.md`](./VERIFICATION.md).

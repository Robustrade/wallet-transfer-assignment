# AI use disclosure — include in the PR

**Tool:** OpenAI ChatGPT (GPT-6 in this conversation).

**How AI was used:** ChatGPT was used extensively throughout this assignment to inspect the assignment and rubric; compare balance, locking, and idempotency approaches; draft and refine the architecture; generate the initial Go project, SQL migration, service/repository transaction code, HTTP layer, integration tests, documentation, and submission checklist; and review the final implementation for correctness risks. This was not only grammar assistance or minor code completion.

**Candidate's role:** The candidate questioned design assumptions, selected the three-table/idempotency and ordered-locking strategy, identified the idempotency-validation-order and test-funding issues, ran the application and tests with real PostgreSQL on Windows, and reported results for each increment. The final changes must be rerun locally before the PR is marked green.

**Full transcript requirement:** Robustrade specifically requires a full transcript of the AI session added to the repository or emailed with the submission. If a transcript export is genuinely unavailable, Robustrade asks for **all prompts used**. The actual full session transcript is **not included in this package**: this document is a summary, not an authentic transcript or complete prompt log. Export/copy the original ChatGPT conversation from your account and include the complete content in the repository as `docs/AI_SESSION_TRANSCRIPT.md` (if acceptable for privacy) **or email it to the recruiting team when submitting**. Update the PR body to specify which option was used and do not mark disclosure complete before delivering it.

The transcript may include personal details, API keys, connection passwords, or unrelated information; review it before sharing. Remove secrets and confidential unrelated material rather than publishing them in a public fork. Do not claim you wrote the generated code unaided, and be ready to explain the design, data model, locking, retries, and known limitations in the follow-up discussion.

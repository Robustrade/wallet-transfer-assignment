## AI Usage

I used **OpenAI Codex** as an AI-assisted development and review tool during this assignment.

### 1. Tool Used

* **OpenAI Codex**

### 2. How I Used AI During the Assignment

I used Codex throughout the assignment as a development assistant for design review, coding support, testing, and verification.

Before implementation, I worked through the **HLD and LLD myself**, including:

* API flow for wallet-to-wallet transfers
* database design for `wallets`, `transfers`, `ledger_entries`, and `idempotency_records`
* transaction boundaries
* idempotency handling
* request hashing and duplicate request behavior
* pessimistic wallet locking using `SELECT ... FOR UPDATE`
* deterministic lock ordering
* prevention of concurrent double spending
* double-entry ledger design
* transfer state transitions
* rollback and failure scenarios
* retry behavior

Once the design was clear, I used Codex to help with parts of the implementation as well. This included generating or refining some Spring Boot code, repository methods, database migration details, request-hash generation, exception handling, and transaction-related code based on the design I had already defined.

I also used Codex to:

* review the repository against `ASSIGNMENT.md`
* review my HLD and LLD and identify missing edge cases
* validate the idempotency and concurrency approach
* review transaction and database invariants
* suggest improvements to Spring Boot/JPA implementation
* assist with database constraints and Flyway migration changes
* assist with unit test creation
* assist with integration and concurrency tests
* identify possible race conditions and failure scenarios
* review the final implementation against the assignment requirements

I did not directly accept all generated code. I reviewed the suggestions, modified code where required, and made the final decisions about the architecture, transaction strategy, locking approach, schema, and failure behavior.

### 3. AI Session / Prompts

A complete AI transcript can be included with the submission where available.

The main prompts used during the development session included:

* `review this repo and ASSIGNMENT.md is the problem statement and expectations`
* `review my HLD and LLD for the wallet transfer service and compare it with the assignment`
* `analyse my understanding of idempotency, concurrency, ledger consistency and safe state transitions`
* `review the database design for wallets, transfers, ledger_entries and idempotency_records`
* `review the transaction boundary for wallet transfer processing`
* `review the idempotency strategy and duplicate request handling`
* `explain and verify how the requestHash should be generated`
* `help refine the Spring Boot implementation for the transfer workflow`
* `review repository methods and JPA locking for wallet updates`
* `review the Flyway migration and database constraints`
* `review concurrent transfers using SELECT FOR UPDATE`
* `check deterministic wallet lock ordering and possible deadlock scenarios`
* `review how two simultaneous transfers from the same wallet should be handled`
* `review ledger consistency and double-entry accounting`
* `review failure scenarios, transaction rollback and partial execution`
* `help me write unit tests for the transfer service`
* `help me add integration tests for idempotency and ledger correctness`
* `help me add concurrency tests for simultaneous wallet transfers`
* `fix P1 — Ledger invariants are not enforced by the database`
* `review the implementation against ASSIGNMENT.md`
* `approve`

Overall, I used AI as a **coding and engineering assistant** to speed up implementation, improve test coverage, review edge cases, and validate the solution. The HLD, LLD, key design choices, and final engineering decisions remained under my ownership.

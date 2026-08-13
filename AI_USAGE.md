# AI Usage Disclosure

## 1. Tool

Claude Code (Anthropic), used as an interactive coding assistant within my editor.

## 2. How I generally used it

I used AI as a **development aid and pair-programming assistant**, rather than delegating the complete implementation to it.

Approximately **40% of the development process involved AI assistance**, while the remaining work—including understanding the requirements, making architectural and implementation decisions, writing and modifying code, debugging, testing, and verifying the final behavior—was performed manually by me.

My general workflow was:

* I first reviewed the assignment requirements and determined the overall approach and architecture myself.
* I decided to use **Go, Gin, PostgreSQL, and a modular/layered architecture** with separate handlers, services, repositories, and domain models.
* I used AI selectively when I needed help with implementation details, boilerplate, debugging, or clarification of Go-specific patterns.
* I reviewed and modified the generated suggestions rather than accepting them blindly.
* A significant portion of the implementation was written and adjusted manually based on my understanding of the assignment.
* I manually tested the APIs and verified the application behavior.
* I ran the build, unit tests, integration tests against a real PostgreSQL container, and a concurrency test involving multiple HTTP requests operating on the same wallet.
* I reviewed the codebase myself to understand the request flow, database interactions, transaction handling, idempotency, and concurrency/locking strategy.
* During the review and testing process, I identified issues and corrected them, including:

  * A timestamp issue where the database-generated `createdAt` value was not being populated back into the Go struct.
  * A missing business rule preventing transfers between wallets using different currencies.

The final implementation was therefore **not treated as an AI-generated submission**. AI was used to accelerate parts of the development process, while I remained responsible for the technical decisions, implementation, debugging, testing, and final verification.

## 3. Prompts / AI assistance used during the project

The AI assistance was primarily used for:

### Understanding the assignment

* Understanding the existing repository and assignment requirements.
* Clarifying the expected wallet-transfer behavior.
* Discussing possible project structures and implementation approaches.

### Implementation assistance

* Getting help with specific Go/Gin implementation details.
* Discussing how to structure handlers, services, repositories, and domain models.
* Getting suggestions for API routing and dependency wiring.
* Assistance with some repetitive/boilerplate implementation work.

### Debugging and verification

* Investigating implementation issues encountered during development.
* Reviewing parts of the implementation for potential problems.
* Helping analyze concurrency behavior and database transaction handling.
* Reviewing the implementation against the assignment requirements.

### Code understanding

After implementation, I used AI to explain parts of the codebase so I could independently understand and defend the implementation during a technical discussion.

This included understanding:

* `main.go` and application startup.
* Gin routing and handlers.
* Handler → Service → Repository flow.
* PostgreSQL interactions.
* Transactions and locking.
* Idempotency handling.
* Wallet balance updates.
* Request payloads, path parameters, and query parameters.
* Go project files such as `go.mod`, `go.sum`, and `Makefile`.
* The reasoning behind the project structure.

## AI's role vs. mine

### AI assistance

AI was used for approximately **40% of the overall development effort**, mainly for:

* Implementation suggestions.
* Boilerplate/repetitive code assistance.
* Debugging assistance.
* Explaining Go/Gin concepts and existing code.
* Reviewing implementation details.
* Helping identify potential edge cases.

### My role

I was responsible for approximately **60% of the overall work**, including:

* Understanding the assignment requirements.
* Deciding the architecture and technology choices.
* Determining the business rules.
* Implementing and modifying code manually.
* Reviewing AI-generated suggestions.
* Debugging and fixing issues.
* Running and interpreting tests.
* Validating API behavior.
* Testing concurrency scenarios.
* Reviewing database transactions and locking behavior.
* Making the final implementation decisions.
* Preparing the repository for submission.
* Ensuring that I understood the complete codebase and could explain the implementation independently.

AI was therefore used as a **coding assistant**, similar to having another developer available for suggestions and explanations, rather than as an autonomous system responsible for building and submitting the project.
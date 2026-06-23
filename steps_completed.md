# Side-by-Side Step Log - Wallet Transfer Service

This file logs each step taken during the planning, implementation, and verification of the Wallet Transfer Service.

| Step # | Action Description | Purpose / Rationale | Status |
|--------|--------------------|---------------------|--------|
| 1      | Checked workspace directory | Determine if files/cloned repo exist. Found empty folder. | Completed |
| 2      | Cloned template repository | Pull the initial repository structure and instructions from `https://github.com/Robustrade/wallet-transfer-assignment.git`. | Completed |
| 3      | Inspected repository guidelines | Read `ASSIGNMENT.md`, `README.md`, `evaluation_guide.md`, and `.github/workflows/ci.yml` to understand rules, constraints, and requirements. | Completed |
| 4      | Verified environment and tech stack | Checked system tools (`java`, `mvn`, `docker`, `sqlite3`, `pg_isready`). Found Java 21 installed. Decided to build a Maven-based Spring Boot project with H2 in PostgreSQL compatibility mode. | Completed |
| 5      | Created `implementation_plan.md` | Created the planning-mode artifact detailing the technical plan for user approval. | Completed |
| 6      | Created local design document `approach.md` | Formulated the core architecture, database tables, concurrency locking (lexicographically ordered pessimistic writes), and idempotency flow. | Completed |
| 7      | Created side-by-side tracker `steps_completed.md` | Track progress of all actions taken for visibility. | Completed |
| 8      | Scaffolded Spring Boot 4 / Java 21 Maven project | Added `pom.xml`, Maven wrapper, H2 config, and layered package structure from `approach.md`. | Completed |
| 9      | Implemented domain, repositories, services, and API | Built entities, pessimistic wallet locking, idempotency records, `POST /transfers`, and exception handling. | Completed |
| 10     | Added behavioral tests and verified build | Added transfer, idempotency, failure, and concurrency tests; all 6 tests pass via `.\mvnw.cmd test`. | Completed |
| 11     | Verified end-to-end execution | Started local server via `.\mvnw.cmd spring-boot:run` to confirm the Tomcat server starts on port 8080 and database seeding works. | Completed |
| 12     | Created IDE, Postman, and DB setup guide | Created `intellij_postman_db_setup.md` detailing project import, IntelliJ run settings, Postman configuration, and H2 database verification. | Completed |
| 13     | Switched and committed to custom branch | Checked out `solution/alkajaiswal` and committed all files (implementation, tests, configurations, logs, and polished README.md). | Completed |
| 14     | Pushed solution branch to GitHub fork | Updated remote origin to point to fork `alka7321/wallet-transfer-assignment` and pushed `solution/alkajaiswal`. | Completed |
| 15     | Configured PostgreSQL Local DB | Switched the database from H2 to PostgreSQL 17 on localhost:5432, adding JDBC driver dependencies to `pom.xml` and configuring user credentials. | Completed |
| 16     | Scaffolded Angular Client | Installed Node.js & Angular CLI and generated a clean Angular Single Page Application `angular-frontend/` for wallet dashboard testing. | Completed |
| 17     | Implemented Angular UI components | Built components in `app.ts`, `app.html`, and `app.css` to load balances, execute transfers, track idempotency, and log historical entries. | Completed |






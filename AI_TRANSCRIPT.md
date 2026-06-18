# AI Usage Transcript

**Tool:** Claude (chat)

**How I used it:** As a step-by-step pair-programming and design partner. I drove
the work — confirming each decision, running all code and tests locally myself,
and asking for explanations of concepts I wanted to understand before moving on.
The assistant generated code under my review one step at a time.

Below are the prompts I used during the session, in order.

---

1. Asked for help with the wallet-transfer assignment, sharing the assignment
   files (ASSIGNMENT.md, README.md, copilot-instructions.md) and screenshots.
   Instructed the assistant to go step by step, not assume code, ask before
   making changes, and act as a senior engineer.

2. Chose the tech stack: Go + standard library + PostgreSQL.

3. Chose Option A: a fresh dedicated Postgres container via docker-compose on
   port 5433.

4. Confirmed one PR with multiple commits (per manager's instruction).

5. Asked whether I needed to use my GitHub username for the Go module path.

6. Created the module, folder structure, and docker-compose; shared a screenshot.

7. Ran docker compose up and created the git branch; shared output.

8. Committed the project scaffold.

9. Confirmed schema design decisions (stored balance, text wallet IDs / UUID
   transfer IDs, BIGINT amounts).

10. Created the migration file and applied it; shared the table list.

11. Committed the schema.

12. Agreed the state-machine logic should live in the domain layer.

13. Ran the domain tests (Red phase); shared the failing output.

14. Ran the domain tests after implementation (Green); shared passing output.

15. Committed the domain layer.

16. Agreed on the repository design: pgx, TxManager pattern (service owns the
    transaction), and ordered SELECT ... FOR UPDATE locking.

17. Ran `go get` for pgx; shared output.

18. Asked how a new key is generated for a new transfer.

19. Confirmed my understanding of when new keys are generated.

20. Agreed with the two-place idempotency strategy (up-front lookup + unique
    constraint).

21. Built the repository layer and confirmed it compiled.

22. Committed the repository layer.

23. Agreed to write the service tests using an in-memory fake repository.

24. Ran the service tests (Red); shared failing output.

25. Ran the service tests after implementation (Green); shared passing output.

26. Asked what `tr.TransitionTo(StatusProcessed)` does and why some interface
    methods are not called on every path.

27. Committed the service layer.

28. Confirmed the HTTP status-code mapping (201 new, 200 duplicate, etc.).

29. Ran the handler/service tests after the signature change; fixed the tests
    and confirmed Green.

30. Confirmed the main.go wiring approach: stdlib mux, env config, graceful
    shutdown.

31. Hit a port 8080 conflict; switched to a configurable port.

32. Decided to use a `.env` file; chose Option A (godotenv).

33. Ran the server reading from `.env`; confirmed it listened on :8090.

34. Asked why wallets are seeded via SQL instead of created via an API; chose to
    keep wallet creation out of scope (Option A).

35. Seeded wallets and ran a transfer via curl; confirmed 201 PROCESSED.

36. Ran the same request again and verified idempotency in the database
    (balances correct, one transfer, two ledger entries).

37. Split the pending changes into two logical commits (handler, then server
    wiring).

38. Agreed on the concurrency integration test design (Option A: skip if DB
    unreachable).

39. Hit the cgo/-race error; ran the test without -race (Option A); confirmed it
    passed.

40. Ran the full test suite; all green.

41. Committed the concurrency test.

42. Chose to keep the template README and add a separate SOLUTION.md (Option B).

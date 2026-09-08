# Evaluation Guide

Use this rubric during pull request review.

## 1. Database Schema

Look for:

- sensible table design
- primary keys and foreign keys
- uniqueness on idempotency keys where appropriate
- useful indexes
- constraints preventing invalid states

Questions:

- Can duplicate transfers happen accidentally?
- Can ledger rows exist without a transfer?
- Is the model easy to reason about?

## 2. Transaction and Locking Strategy

Look for:

- explicit transaction boundaries
- safe locking or concurrency control
- prevention of double spend
- clear handling of insufficient funds

Questions:

- Are concurrent debits on the same wallet safe?
- Is there any read-then-write race?
- Does the code rely on application logic where the database should enforce correctness?

## 3. Idempotency

Look for:

- durable storage of idempotency key
- safe replay behavior
- same response returned for duplicate requests where reasonable
- no repeated side effects

Questions:

- What happens if the same request is sent twice?
- What happens if the first request commits and the response is lost?
- Is the idempotency strategy safe across process restarts?

## 4. Layering and Code Quality

Look for:

- thin handlers
- business logic in service layer
- repositories limited to persistence concerns
- clear naming and understandable flow

Questions:

- Is the code easy to extend?
- Are responsibilities mixed across layers?
- Is the solution simpler than it needs to be?

## 5. Testing

Look for:

- unit and service-level tests where appropriate
- tests for duplicate requests
- tests for ledger balancing
- tests for failed transfers
- concurrency test coverage if feasible

Questions:

- Do tests describe behavior clearly?
- Are important failure cases covered?
- Would the tests catch a regression in idempotency or double spending?

## 6. Development practices

Look for:

- Properly crafted topical commits with proper commit messages
- Proper naming for functions, variables, and files
- Good documentation (including the README, commit messages as already mentioned, PR note etc.)


## 7. Overall Recommendation

Suggested ratings:

- Strong hire
- Hire
- Mixed
- No hire

Capture short notes on:

- strengths
- risks
- follow-up questions for interview

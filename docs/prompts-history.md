# Prompts History Log

## Prompt #1

You are Golang expert and tasked with creating a money transfer service among digital wallet. First, read assignment instructions, chalk down execution plan, and seek approval. 

## Prompt #2 - Feedback

Include SonarQube CE container with Uber Golang style guide.
Use UUID v7 and make sure it is generated in domain layer and passed on.
Focus on building the solution inside-out approach using clean architecture and SOLID principles. While building each layer, commit code changes with commit message summarizing the change. Before commit, make sure code is linted, formatted, gets success from SonarQube, and has working functional tests.
Introduce a Make file with commands to build and test inside Docker Compose containers.
Update the plan and seek approval.

## Prompt #3 - Feedback

Include indexes created in the plan as well as database migration scripts. Use integer based status codes with constants mapped in Domain models.
Moreover, use git branch name "waseem". 
Furthermore, "slog" standard package for logging and log state transitions with idempotency-key and relevant identifiers as log fields. Log errors with short layer name.
Furthermore, use Test Driven Development approach and before making code changes, write a test which should fail initially, commit only test mentioning why test is failing. Next, complete the implementation in suitable layer(s) and adjust the test accordingly before committing layer changes.
Lastly, read documentation-first workflow and make a markdown doc for every commit answering sought questions.
Update the plan and seek approval.

---

Plan approved to avoid hallucinations and 20% credits consumed

---

## Review 

Next, using file ".github/copilot-instructions.md" review code base and create suitable fixes, once again do not push any code changes yet.

>>>
All done. Here's a summary of what was fixed against the copilot-instructions review criteria:

---

### Fixes committed (`db61a04`)

| Category | Issue | Fix |
|----------|-------|-----|
| **Correctness** | No idempotency conflict detection — key reuse with different params returned cached response silently | SHA-256 hash of `(fromWallet\|toWallet\|amount)` stored in `idempotency_records`; mismatch → `ErrDuplicateIdempotencyKey` → HTTP 409 |
| **Correctness** | `domain.CanTransition` defined but never called — state machine guard was dead code | Service now calls it before `UpdateStatus`; `ErrInvalidTransition` is now reachable |
| **Layer hygiene** | Repo constructors took `interface{}` and discarded it | All 4 constructors now accept `*slog.Logger`; repos use injected logger (consistent with service) |
| **Layer hygiene** | `idempotencyRepo.Get` returned `domain.ErrTransferNotFound` for a missing cache entry | Added `domain.ErrIdempotencyKeyNotFound`; used in the repo |
| **API ergonomics** | `TransferStatus` serialized as integer (`2`) in JSON | Added `MarshalJSON`/`UnmarshalJSON` — API now returns `"PROCESSED"` etc. |
| **Handler mapping** | No HTTP 409 for duplicate-key conflict | `writeServiceError` maps `ErrDuplicateIdempotencyKey` → 409 Conflict |
| **Schema** | `request_hash` column missing from `idempotency_records` | Migration 000003 adds it |

**Test count: domain 9 + handler 14 + service 14 = 37 unit tests, all passing** ✅
>>>

## Prompt for Magic endpoint

You are golang code generator, help me create a new endpoint which deposits random amount between 100 and 10000 cents to a given wallet-id. 
You will introduce a wallet with balance of a million cents. This wallet should be created by a migration script and it should have a static UUID. This UUID should be stored in Domain layer with constant named as NatureWalletID. 
Next, create a HTTP endpoint "/magic" with request payload similar to that of "transfer" endpoint but without "fromWalletId" and "amount" fields. The response body should be the same (ie TransferResponse object).
The handler layer does similar validation for idempotencyKey and toWalletId fields, and pass on to service layer.
The service layer should execute transfer request by constructing TransferRequest object with incoming data plus using NatureWalletID as value for fromWalletId field and amount will be random value between 100 and 10000 cents.
Write suitable tests, document details as done previously, also update README.md with API specs and sample curl request. Commit code changes with brief summary of actions taken separately for each layer. Make changes with inside-out approach.
Plan and seek approval

## Prompt for Create Wallet endpoint

You are golang code generator, I need another API to create new wallet. 
Similar to Get Wallet balance API, I need POST method to create a new wallet. There should not be any body for the request and response body will return wallet-ID.
The service layer will generate Wallet-ID, use suitable repository to persist with default balance of 0 cents.
As usual, write suitable tests, document details as done previously, also update README.md with API specs and sample curl request. 
Make sure to re-order sample requests as follow: 
1, Create Wallet API
2, Magic API
3, Wallet Balance API
4, Initiate Transfer API
5, Transfer status API
Commit code changes with brief summary of actions taken separately for each layer. Make changes with inside-out approach.
Plan and seek approval

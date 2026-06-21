# AI Usage Note

## Tool used

ChatGPT

## How I used it

I used ChatGPT as a planning, review, and implementation-support assistant while working on this assignment.

I worked design-first. I first prepared the initial approach and decided the main solution direction myself: wallet-to-wallet transfer flow, idempotency handling, transaction boundaries, ledger entries, duplicate request handling, and failed transfer handling.

I used AI mainly to improve clarity, validate edge cases, and speed up repetitive implementation work.

Specifically, I used it to:

* Review the transfer flow from the initial approach document.
* Improve the clean service flow for successful and failed transfers.
* Reason about retry-safe behavior using a separate idempotency record.
* Decide how expected business failures like insufficient balance should be recorded.
* Review error scenarios such as invalid wallet, insufficient balance, duplicate request, and concurrent debit attempts.
* Plan test cases based on the approach document.
* Generate and refine integration test scenarios for success, failure, duplicate idempotency key, ledger entry validation, and concurrent transfer cases.
* Improve documentation wording for README and implementation explanation.
* Review local test failures at a high level and correct the implementation accordingly.

I reviewed and corrected the suggestions before applying them. I implemented, ran, debugged, and verified the final solution locally.

## Representative prompts

These are representative prompts similar to what I used during the session:

1. Help me understand the wallet transfer assignment expectations around idempotency, retries, concurrency, transaction safety, database design, and testing.

2. Help me structure an initial approach document for a wallet-to-wallet transfer assignment where duplicate requests, retries, failed transfers, and concurrent debits need to be handled correctly.

3. Review this successful transfer flow: validate request, claim idempotency key, create transfer as pending, lock involved wallets, debit source, credit destination, create ledger entries, mark transfer processed, and store the final result.

4. Review this failed transfer flow: if the source wallet has insufficient balance, mark the transfer as failed, do not create ledger entries, and store the failed result so retries return the same response.

5. Help me decide whether a business failure should be rolled back or committed as a failed transfer record.

6. Help me keep the controller thin and move the main business logic into the service layer.

7. Review the clean code flow for the transfer service so that idempotency handling, validation, wallet updates, ledger entries, and response creation are easy to follow.

8. Help me identify possible error scenarios for this wallet transfer service, including invalid wallet, insufficient balance, duplicate request, and concurrent transfer attempts.

9. Help me design integration tests based on my approach document for successful transfer, insufficient balance, invalid wallet, duplicate idempotency key, ledger entries, and concurrent transfers from the same wallet.

10. Help me refine the test assertions so they verify balances, transfer status, ledger entries, and idempotency behavior correctly.

11. Help me prepare a concise README explaining the implementation, idempotency strategy, transaction strategy, concurrency handling, API, and tests.

## Validation

I ran the test suite locally and verified that the implemented scenarios passed before submitting the solution.

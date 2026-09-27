1. Validate the transfer request.
2. Handle idempotency using the provided idempotency key.
3. Lock/serialize balance updates for the involved wallets.
4. Verify the sender has sufficient funds.
5. Create corresponding debit and credit ledger entries.
6. Update wallet balances atomically.
7. Store the result associated with the idempotency key.
8. Return the same result for repeated requests with the same key.


Concurrent transfers involving the same wallet must be serialized so that two requests cannot both observe the same balance and overspend it.



Use double-entry accounting:

Debit sender wallet
Credit receiver wallet

The total value of every completed transfer must balance between the two entries.

Tests should cover:

Successful transfer
Insufficient balance
Invalid wallet
Same-wallet transfer
Duplicate/idempotent request
Concurrent transfers
Ledger balance
Atomicity/error cases

INITIAL ANALYSIS:

DB schema:

    Transfers(
        id
        idempotency_key
        from_wallet_id
        to_wallet_id
        amount
        status
        created_at
        updated_at
    )

    LedgerEntries(
        id
        transfer_id
        wallet_id
        amount
        created_at
    )

    Wallet(
        wallet_id
        balance
        version
        updated_at
    )



Approach for concurrent requests to the same wallet:
    - Pessimistic locking:
        Lock transaction, block other transaction and sequentially execute.
        Pros : 
            - No inconsistency
            - No race condition
        Cons : 
            - Hot wallet problem
            - Throughput drop/ latency increase
    - Kafka + optimistic locking:
        - Ledger taken as source of truth.
        - Topic partitioning per wallet for ordering guarantee per wallet and asynchronous balance update.
        - Locking done on balance aggregation with version(on a single wallet) not for each transaction.

distributed transaction system with
    idempotency
    concurrency
    ledger consistency
    safe state transitions

implement
    database design
    transaction safety
    retry-safe behavior
    clean architecture
    testing discipline

implement == wallet transfer  --> take care of
    Idempotent request handling
    Double-entry ledger recording
    Correct balance tracking
    Safe concurrent execution


Create Transfer
    POST /transfers
    { "idempotencyKey": "abc123", "fromWalletId": "wallet_1", "toWalletId": "wallet_2", "amount": 100 }
    avoid duplicate transfer using idempotency key
    atomically transfer (ACID)

wallet balance
    deriving balance from the ledger  or
    maintaining a stored balance updated during transfers
    correct balance under concurrent request

double entry ledger
    entry_id	wallet_id	transfer_id	type	amount
    1	wallet_1	T1	DEBIT	100
    2	wallet_2	T1	CREDIT	100

    every transfer must generate exactly two entries 
    debit from source wallet 
    credit to destination 
    wallet ledger must always balance

Transfer state
    state machine --> states (PENDING, PROCESSED, FAILED)
    PENDING -> PROCESSED
    PENDING -> FAILED  --> only two transition are
    State transitions must be safe under retries and duplicates.

concurrency safety
    Two transfers attempt to debit the same wallet simultaneously
    Possible approaches include:
    database transactions
    row-level locks
    optimistic locking
    isolation levels
    You should choose and justify your strategy.


Database --> postgres
    tables --> wallets, transfers, ledger_entries, idempotency_records
    add more tables if needed

Architecture Expectations
    follow a clean layered architecture.
    handler layer (request validation, transport mapping, invoking service logic), 
    service layer(business logic, orchestration, idempotency behavior, transfer workflow), 
    repository layer (database operation), 
    domain models (entities, state transitions, validation rules)


idempotency requirement
    You should clearly define:
        how idempotencyKey is stored
        how duplicate requests are detected
        how the system returns the original result
        how duplicate side effects are prevented
    
    Possible mechanisms include:
        unique database constraints
        idempotency tables
        guarded state transitions

Distributed system
    Assume the system may experience:
        duplicate requests
        retries
        network failures
        partial execution
    
    Your design should consider:
        retry-safe operations
        safe state transitions
        transactional boundaries
        failure handling
    The system must behave correctly under retries and duplicate delivery.



Database Design
    schema clarity
    constraints
    data integrity
    index usage

Transaction Strategy
    transaction boundaries
    lock strategy
    race condition handling

Idempotency Strategy
    correctness under retries
    deduplication guarantees








POST /transfers
       ↓
Validate DTO
       ↓
Check idempotency
       ↓
Begin DB transaction
       ↓
Insert transfer (PENDING)
       ↓
Lock wallets
       ↓
Validate balance
       ↓
Update balances
       ↓
Insert debit ledger
       ↓
Insert credit ledger
       ↓
Mark transfer PROCESSED
       ↓
Save idempotency response
       ↓
Commit



row level lock --
    always lock in determistic way --> smaller id pe pehle then loarger id pe lock


transfer state machine
    use function to update state

idempotency strategy
    first request --> insert idempotency and in_progress
    if duplicate request
        if completed --> return 200
    
    in progress --> 409
    failed --> retry

transaction
    transfer row
    wallet balance update
    ledger entries
    transfer state update
    idempotency response update


sum rule for ledger == 0  credit - debit for all = 0




type Wallet struct {
	ID        uint64
	WalletID  string
	Balance   float64
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}



type TransferState string

const (
	Pending   TransferState = "PENDING"
	Processed TransferState = "PROCESSED"
	Failed    TransferState = "FAILED"
)

type Transfer struct {
	ID             uint64
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         float64
	State          TransferState
	FailureReason  *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type EntryType string

const (
	Debit  EntryType = "DEBIT"
	Credit EntryType = "CREDIT"
)

type LedgerEntry struct {
	ID         uint64
	WalletID   string
	TransferID uint64
	Type       EntryType
	Amount     float64
	CreatedAt  time.Time
}
type Status string

const (
	InProgress Status = "IN_PROGRESS"
	Completed  Status = "COMPLETED"
	Failed     Status = "FAILED"
)

type IdempotencyRecord struct {
	ID             uint64
	IdempotencyKey string
	RequestHash    string
	TransferID     uint64
	Response       []byte
	Status         Status
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
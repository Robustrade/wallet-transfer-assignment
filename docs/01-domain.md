# Domain Layer Design

## Problem Statement

The domain layer defines the core business entities, value types, and rules for
the wallet transfer service. It is the innermost layer and has **no external
dependencies** — it does not import persistence, transport, or third-party
libraries except for UUID generation.

## Expected Behaviour

| Entity | Responsibility |
|---|---|
| `Wallet` | Holds a monetary balance. A new wallet starts at zero. |
| `Transfer` | Represents a money movement request between two wallets. Always starts in `PENDING` state. |
| `LedgerEntry` | Records one side of a double-entry bookkeeping pair (DEBIT or CREDIT). |
| `Amount` | Integer minor-unit value (e.g. cents) to avoid floating-point rounding. |
| `TransferStatus` | Integer-coded state: `1=PENDING`, `2=PROCESSED`, `3=FAILED`. |
| `EntryType` | Integer-coded direction: `1=DEBIT`, `2=CREDIT`. |

## Status Codes

Integer codes are stored in the database to keep index size small and decouple
display strings from persistence:

```go
TransferStatusPending   = 1
TransferStatusProcessed = 2
TransferStatusFailed    = 3

EntryTypeDebit  = 1
EntryTypeCredit = 2
```

Each type implements `String()` for logging and JSON output.

## Transfer State Machine

```
PENDING ──→ PROCESSED
PENDING ──→ FAILED
```

`CanTransition(src, dst)` enforces that only these two paths are valid.
Terminal states (`PROCESSED`, `FAILED`) have no outgoing transitions.

## UUID v7 Strategy

`NewID()` in `internal/domain/id.go` wraps `uuid.NewV7()` from
`github.com/google/uuid`. UUID v7 values are:

- **Time-ordered** — natural sort order in the DB without an extra `created_at`
  index for pagination.
- **Generated in the domain layer** — callers know the ID before any DB call,
  enabling optimistic insert patterns and idempotency checks.

`ZeroID()` returns `uuid.UUID{}` as the sentinel "not set" value.

## Side Effects

The domain layer is **pure** — no I/O, no side effects. `NewID()` reads from
`crypto/rand`; a panic is raised only if the OS PRNG is unavailable (catastrophic
system failure, not a normal runtime error).

## Failure Modes

Sentinel errors in `errors.go` are used across all service boundaries:

| Error | When raised |
|---|---|
| `ErrWalletNotFound` | Wallet UUID does not exist in persistence. |
| `ErrTransferNotFound` | Transfer UUID does not exist in persistence. |
| `ErrInsufficientFunds` | Source wallet balance < transfer amount. |
| `ErrInvalidAmount` | Transfer amount ≤ 0. |
| `ErrSameWallet` | Source and destination wallet IDs are equal. |
| `ErrInvalidTransition` | A state transition violates the state machine. |
| `ErrDuplicateIdempotencyKey` | Same key submitted with different parameters. |

## Idempotency Behaviour

The domain layer itself does not store or check idempotency — that is the
responsibility of the service layer. The domain provides the `Transfer` entity
with an `IdempotencyKey` field that the service and repository layers use.

## Consistency Expectations

All domain constructors (`NewWallet`, `NewTransfer`, `NewLedgerEntry`) assign
timestamps in UTC. The repository layer stores and reads back these values with
full timezone information to avoid ambiguity.

## Testing Strategy

Unit tests in `internal/domain/domain_test.go` cover:

- UUID v7 version check and uniqueness.
- `TransferStatus.String()` and `EntryType.String()` round-trips.
- `CanTransition` exhaustive edge cases.
- Constructor invariants for `Transfer`, `Wallet`, and `LedgerEntry`.

Tests are table-driven, parallel, and have **zero external dependencies**.

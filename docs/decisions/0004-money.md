# ADR 0004: Integer minor units

## Status

Accepted

## Context

The sample request uses `"amount": 100` with no currency and no scale. Floating point cannot represent money safely.

## Decision

Amounts are `int64` in Go and `bigint` in PostgreSQL. The API accepts only JSON integers. Values must be from `1` through `1_000_000_000_000_000` for transfers. Opening balances may be `0` through the same maximum.

There is one implicit currency. The service does not convert or round.

The credit path refuses a balance that would overflow `int64`. PostgreSQL also raises `numeric_value_out_of_range` on `bigint` overflow, and the wallet `CHECK (balance >= 0)` rejects a negative result.

## Consequences

- Clients that need cents send cents. Clients that treat `100` as whole currency units can do that too. The service does not interpret a scale.
- Fractional JSON such as `100.50` is a validation error.

## Alternatives

`numeric(20, 4)` is a common ledger type when a scale is part of the contract. The assignment does not define a scale, and integer minor units keep comparisons, locks, and tests exact. Introducing a scale later would be a new API version, not a silent change to this one.

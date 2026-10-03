# ADR-003: Record opening balances on the wallet

- **Status:** Accepted
- **Date:** 2026-10-03
- **Design doc:** [§4 Data Model](../design.md#4-data-model)

## Context

Wallets get their opening balances from seed data; there is no funding API
(requirements D-7). Those funds do not come from transfers, yet the ledger
must always balance and every balance must be explainable.

## Decision

Add `wallets.opening_balance`, set once at seed time and never changed. The
ledger records only transfers.

## Consequences

- Every balance is explainable: `balance = opening_balance + credits − debits`.
- The ledger balances on its own, because every entry pair comes from one
  transfer.
- No system wallet is needed, so no wallet ever goes below zero (FR-2).
- The ledger alone does not show where opening funds came from; that lives in
  the seed data.
- A future funding API would need its own way to appear in the ledger.

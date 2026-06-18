package com.wallet.transfer.domain;

/**
 * Ledger Entry Type Enumeration
 * 
 * Represents the type of ledger entry for double-entry bookkeeping.
 * 
 * Types:
 * - DEBIT: Withdrawal from a wallet (source wallet in a transfer)
 * - CREDIT: Deposit to a wallet (destination wallet in a transfer)
 * 
 * Invariant: Every transfer must produce exactly 2 entries:
 *   1 DEBIT entry from source wallet
 *   1 CREDIT entry to destination wallet
 *   Both entries must have the same amount.
 */
public enum LedgerEntryType {
    DEBIT,
    CREDIT
}

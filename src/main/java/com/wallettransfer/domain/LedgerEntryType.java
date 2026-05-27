package com.wallettransfer.domain;

/**
 * Side of a double-entry ledger row for a transfer.
 */
public enum LedgerEntryType {
    /** Money leaving the source wallet. */
    DEBIT,
    /** Money entering the destination wallet. */
    CREDIT
}

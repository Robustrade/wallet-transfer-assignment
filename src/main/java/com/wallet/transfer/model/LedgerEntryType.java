package com.wallet.transfer.model;

/**
 * Enum defining the type of double-entry ledger record.
 */
public enum LedgerEntryType {
    /**
     * Represents a deduction (outflow) of funds from a wallet.
     */
    DEBIT,

    /**
     * Represents an addition (inflow) of funds into a wallet.
     */
    CREDIT
}

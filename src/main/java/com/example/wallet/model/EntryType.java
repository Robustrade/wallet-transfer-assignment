package com.example.wallet.model;

/**
 * Direction of a ledger entry in the double-entry bookkeeping system.
 *
 * Every transfer produces exactly one DEBIT and one CREDIT entry.
 */
public enum EntryType {
    DEBIT,
    CREDIT
}


package com.wallet.domain;

import java.time.Instant;
import java.util.UUID;

/**
 * One side of a double-entry bookkeeping record. Every transfer produces two
 * entries: a DEBIT on the source wallet and a CREDIT on the destination.
 *
 * <p>{@code createdAt} is null until the row is persisted — the database
 * supplies it via {@code DEFAULT now()} and the repository surfaces it via
 * {@code RETURNING}. The DB clock is the single source of truth for ledger
 * ordering.
 */
public class LedgerEntry {

    private UUID id;
    private String walletId;
    private UUID transferId;
    private EntryType entryType;
    private Long amount;
    private Long runningBalance;
    private Instant createdAt;

    public LedgerEntry(String walletId, UUID transferId, EntryType entryType, Long amount, Long runningBalance) {
        if (amount <= 0) {
            throw new IllegalArgumentException("Ledger entry amount must be positive");
        }
        this.id = UUID.randomUUID();
        this.walletId = walletId;
        this.transferId = transferId;
        this.entryType = entryType;
        this.amount = amount;
        this.runningBalance = runningBalance;
        // createdAt stays null until the DB assigns it via RETURNING.
        this.createdAt = null;
    }

    /** Reconstruction constructor for repository loading. */
    public LedgerEntry(UUID id, String walletId, UUID transferId, EntryType entryType, Long amount, Long runningBalance, Instant createdAt) {
        this.id = id;
        this.walletId = walletId;
        this.transferId = transferId;
        this.entryType = entryType;
        this.amount = amount;
        this.runningBalance = runningBalance;
        this.createdAt = createdAt;
    }

    public UUID getId() {
        return id;
    }

    public String getWalletId() {
        return walletId;
    }

    public UUID getTransferId() {
        return transferId;
    }

    public EntryType getEntryType() {
        return entryType;
    }

    public Long getAmount() {
        return amount;
    }

    public Long getRunningBalance() {
        return runningBalance;
    }

    public Instant getCreatedAt() {
        return createdAt;
    }
}

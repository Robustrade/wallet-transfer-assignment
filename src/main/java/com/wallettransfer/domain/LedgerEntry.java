package com.wallettransfer.domain;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.EnumType;
import jakarta.persistence.Enumerated;
import jakarta.persistence.GeneratedValue;
import jakarta.persistence.GenerationType;
import jakarta.persistence.Id;
import jakarta.persistence.Table;
import java.time.Instant;

/**
 * One row in the double-entry ledger for a transfer (DEBIT or CREDIT).
 * At most one row per (transfer, type) is enforced in the database.
 */
@Entity
@Table(name = "ledger_entries")
public class LedgerEntry {

    @Id
    @GeneratedValue(strategy = GenerationType.IDENTITY)
    private Long id;

    @Column(name = "wallet_id", nullable = false)
    private String walletId;

    @Column(name = "transfer_id", nullable = false)
    private String transferId;

    @Enumerated(EnumType.STRING)
    @Column(nullable = false)
    private LedgerEntryType type;

    @Column(nullable = false)
    private long amount;

    @Column(name = "created_at", nullable = false)
    private Instant createdAt;

    /** Required by JPA. */
    protected LedgerEntry() {}

    /**
     * Creates a ledger row before persistence (id assigned on save).
     */
    public LedgerEntry(String walletId, String transferId, LedgerEntryType type, long amount, Instant createdAt) {
        this.walletId = walletId;
        this.transferId = transferId;
        this.type = type;
        this.amount = amount;
        this.createdAt = createdAt;
    }

    public Long getId() {
        return id;
    }

    public String getWalletId() {
        return walletId;
    }

    public String getTransferId() {
        return transferId;
    }

    public LedgerEntryType getType() {
        return type;
    }

    public long getAmount() {
        return amount;
    }

    public Instant getCreatedAt() {
        return createdAt;
    }
}

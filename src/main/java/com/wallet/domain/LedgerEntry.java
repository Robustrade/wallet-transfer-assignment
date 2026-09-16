package com.wallet.domain;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.EnumType;
import jakarta.persistence.Enumerated;
import jakarta.persistence.Id;
import jakarta.persistence.PrePersist;
import jakarta.persistence.Table;

import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;

@Entity
@Table(name = "ledger_entries")
public class LedgerEntry {

    @Id
    @Column(name = "id")
    private UUID id;

    @Column(name = "transfer_id", nullable = false)
    private UUID transferId;

    @Column(name = "wallet_id", nullable = false)
    private String walletId;

    @Enumerated(EnumType.STRING)
    @Column(name = "entry_type", nullable = false, length = 8)
    private LedgerEntryType entryType;

    @Column(name = "amount", nullable = false, precision = 19, scale = 4)
    private BigDecimal amount;

    @Column(name = "created_at", nullable = false)
    private Instant createdAt;

    protected LedgerEntry() {
        // JPA
    }

    private LedgerEntry(UUID transferId, String walletId, LedgerEntryType entryType, BigDecimal amount) {
        this.id = UUID.randomUUID();
        this.transferId = transferId;
        this.walletId = walletId;
        this.entryType = entryType;
        this.amount = amount;
    }

    public static LedgerEntry debit(UUID transferId, String walletId, BigDecimal amount) {
        return new LedgerEntry(transferId, walletId, LedgerEntryType.DEBIT, amount);
    }

    public static LedgerEntry credit(UUID transferId, String walletId, BigDecimal amount) {
        return new LedgerEntry(transferId, walletId, LedgerEntryType.CREDIT, amount);
    }

    @PrePersist
    protected void onCreate() {
        this.createdAt = Instant.now();
    }

    public UUID getId() {
        return id;
    }

    public UUID getTransferId() {
        return transferId;
    }

    public String getWalletId() {
        return walletId;
    }

    public LedgerEntryType getEntryType() {
        return entryType;
    }

    public BigDecimal getAmount() {
        return amount;
    }

    public Instant getCreatedAt() {
        return createdAt;
    }
}
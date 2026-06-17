package com.wallettransfer.ledger.domain;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.EnumType;
import jakarta.persistence.Enumerated;
import jakarta.persistence.GeneratedValue;
import jakarta.persistence.GenerationType;
import jakarta.persistence.Id;
import jakarta.persistence.Table;
import jakarta.persistence.UniqueConstraint;
import java.time.Instant;
import java.util.UUID;

@Entity
@Table(
        name = "ledger_entries",
        uniqueConstraints = @UniqueConstraint(columnNames = {"transfer_id", "wallet_id", "entry_type"})
)
public class LedgerEntry {

    @Id
    @GeneratedValue(strategy = GenerationType.IDENTITY)
    private Long id;

    @Column(name = "transfer_id", nullable = false)
    private UUID transferId;

    @Column(name = "wallet_id", nullable = false, length = 64)
    private String walletId;

    @Enumerated(EnumType.STRING)
    @Column(name = "entry_type", nullable = false, length = 10)
    private LedgerEntryType entryType;

    @Column(nullable = false)
    private long amount;

    @Column(nullable = false)
    private Instant createdAt = Instant.now();

    protected LedgerEntry() {
    }

    public LedgerEntry(UUID transferId, String walletId, LedgerEntryType entryType, long amount) {
        this.transferId = transferId;
        this.walletId = walletId;
        this.entryType = entryType;
        this.amount = amount;
    }

    public Long getId() {
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

    public long getAmount() {
        return amount;
    }

    public Instant getCreatedAt() {
        return createdAt;
    }
}

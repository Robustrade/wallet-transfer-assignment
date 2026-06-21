package com.wallet.transfer.domain;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.EnumType;
import jakarta.persistence.Enumerated;
import jakarta.persistence.GeneratedValue;
import jakarta.persistence.GenerationType;
import jakarta.persistence.Id;
import jakarta.persistence.Table;
import java.time.Instant;

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
    @Column(name = "entry_type", nullable = false)
    private LedgerEntryType entryType;

    @Column(nullable = false)
    private Long amount;

    @Column(name = "created_at", nullable = false, updatable = false)
    private Instant createdAt;

    protected LedgerEntry() {
    }

    public LedgerEntry(String walletId, String transferId, LedgerEntryType entryType, Long amount) {
        this.walletId = walletId;
        this.transferId = transferId;
        this.entryType = entryType;
        this.amount = amount;
        this.createdAt = Instant.now();
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

    public LedgerEntryType getEntryType() {
        return entryType;
    }

    public Long getAmount() {
        return amount;
    }

    public Instant getCreatedAt() {
        return createdAt;
    }
}

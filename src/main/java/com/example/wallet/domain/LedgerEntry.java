package com.example.wallet.domain;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.EnumType;
import jakarta.persistence.Enumerated;
import jakarta.persistence.GeneratedValue;
import jakarta.persistence.GenerationType;
import jakarta.persistence.Id;
import jakarta.persistence.Index;
import jakarta.persistence.Table;
import java.math.BigDecimal;
import java.time.Instant;
import org.hibernate.annotations.CreationTimestamp;

@Entity
@Table(
        name = "ledger_entries",
        indexes = @Index(name = "idx_ledger_transfer_id", columnList = "transfer_id"))
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

    @Column(nullable = false, precision = 19, scale = 4)
    private BigDecimal amount;

    @CreationTimestamp
    @Column(nullable = false, updatable = false)
    private Instant createdAt;

    protected LedgerEntry() {
    }

    public LedgerEntry(String walletId, String transferId, LedgerEntryType type, BigDecimal amount) {
        this.walletId = walletId;
        this.transferId = transferId;
        this.type = type;
        this.amount = amount;
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

    public BigDecimal getAmount() {
        return amount;
    }

    public Instant getCreatedAt() {
        return createdAt;
    }
}

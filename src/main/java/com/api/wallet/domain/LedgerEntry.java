package com.api.wallet.domain;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.EnumType;
import jakarta.persistence.Enumerated;
import jakarta.persistence.Id;
import jakarta.persistence.Table;
import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;
import lombok.AccessLevel;
import lombok.Getter;
import lombok.NoArgsConstructor;

@Entity
@Table(name = "ledger_entries")
@Getter
@NoArgsConstructor(access = AccessLevel.PROTECTED)
public class LedgerEntry {

    @Id
    @Column(name = "id", nullable = false, updatable = false)
    private UUID id;

    @Column(name = "transfer_id", nullable = false, updatable = false)
    private UUID transferId;

    @Column(name = "wallet_id", nullable = false, updatable = false)
    private String walletId;

    @Enumerated(EnumType.STRING)
    @Column(name = "entry_type", nullable = false, updatable = false)
    private EntryType entryType;

    @Column(name = "amount", nullable = false, updatable = false)
    private BigDecimal amount;

    @Column(name = "created_at", nullable = false, updatable = false)
    private Instant createdAt;

    private LedgerEntry(UUID transferId, String walletId, EntryType entryType, BigDecimal amount) {
        if (amount == null || amount.compareTo(BigDecimal.ZERO) <= 0) {
            throw new IllegalArgumentException("Ledger entry amount must be positive");
        }
        this.id = UUID.randomUUID();
        this.transferId = transferId;
        this.walletId = walletId;
        this.entryType = entryType;
        this.amount = amount;
        this.createdAt = Instant.now();
    }

    public static LedgerEntry debit(UUID transferId, String walletId, BigDecimal amount) {
        return new LedgerEntry(transferId, walletId, EntryType.DEBIT, amount);
    }

    public static LedgerEntry credit(UUID transferId, String walletId, BigDecimal amount) {
        return new LedgerEntry(transferId, walletId, EntryType.CREDIT, amount);
    }
}

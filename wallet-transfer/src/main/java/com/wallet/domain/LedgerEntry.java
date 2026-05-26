package com.wallet.domain;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.EnumType;
import jakarta.persistence.Enumerated;
import jakarta.persistence.GeneratedValue;
import jakarta.persistence.GenerationType;
import jakarta.persistence.Id;
import jakarta.persistence.Table;
import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;
import lombok.Getter;
import lombok.NoArgsConstructor;

@Entity
@Table(name = "ledger_entries")
@Getter
@NoArgsConstructor
public class LedgerEntry {

    @Id
    @GeneratedValue(strategy = GenerationType.UUID)
    private UUID id;

    @Column(name = "wallet_id", nullable = false, length = 50)
    private String walletId;

    @Column(name = "transfer_id", nullable = false)
    private UUID transferId;

    @Enumerated(EnumType.STRING)
    @Column(name = "entry_type", nullable = false, length = 10)
    private EntryType entryType;

    @Column(nullable = false, precision = 19, scale = 4)
    private BigDecimal amount;

    @Column(name = "created_at", nullable = false, updatable = false)
    private Instant createdAt;

    public static LedgerEntry debit(String walletId, UUID transferId, BigDecimal amount) {
        return create(walletId, transferId, EntryType.DEBIT, amount);
    }

    public static LedgerEntry credit(String walletId, UUID transferId, BigDecimal amount) {
        return create(walletId, transferId, EntryType.CREDIT, amount);
    }

    private static LedgerEntry create(String walletId, UUID transferId, EntryType type, BigDecimal amount) {
        LedgerEntry entry = new LedgerEntry();
        entry.walletId = walletId;
        entry.transferId = transferId;
        entry.entryType = type;
        entry.amount = amount;
        entry.createdAt = Instant.now();
        return entry;
    }
}

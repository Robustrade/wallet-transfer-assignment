package com.example.wallettransfer.domain;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.EnumType;
import jakarta.persistence.Enumerated;
import jakarta.persistence.Id;
import jakarta.persistence.Table;
import lombok.Getter;
import lombok.NoArgsConstructor;

import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;

@Entity
@Getter
@NoArgsConstructor(access = lombok.AccessLevel.PROTECTED)
@Table(name = "ledger_entries")
public class LedgerEntry {

    @Id
    @Column(name = "id", nullable = false)
    private UUID id;

    @Column(name = "wallet_id", nullable = false, length = 64)
    private String walletId;

    @Column(name = "transfer_id", nullable = false)
    private UUID transferId;

    @Enumerated(EnumType.STRING)
    @Column(name = "entry_type", nullable = false, length = 16)
    private LedgerEntryType type;

    @Column(name = "amount", nullable = false, precision = 19, scale = 4)
    private BigDecimal amount;

    @Column(name = "created_at", nullable = false)
    private Instant createdAt;

    private LedgerEntry(UUID transferId, String walletId, LedgerEntryType type, BigDecimal amount) {
        this.id = UUID.randomUUID();
        this.transferId = transferId;
        this.walletId = walletId;
        this.type = type;
        this.amount = amount;
        this.createdAt = Instant.now();
    }

    public static LedgerEntry debit(UUID transferId, String walletId, BigDecimal amount) {
        return new LedgerEntry(transferId, walletId, LedgerEntryType.DEBIT, amount);
    }

    public static LedgerEntry credit(UUID transferId, String walletId, BigDecimal amount) {
        return new LedgerEntry(transferId, walletId, LedgerEntryType.CREDIT, amount);
    }

}

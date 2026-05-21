package com.kullu.wallet.entity;

import java.time.Instant;
import java.util.UUID;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.EnumType;
import jakarta.persistence.Enumerated;
import jakarta.persistence.GeneratedValue;
import jakarta.persistence.GenerationType;
import jakarta.persistence.Id;
import jakarta.persistence.Table;
import jakarta.persistence.UniqueConstraint;

import org.hibernate.annotations.CreationTimestamp;

import lombok.AccessLevel;
import lombok.Getter;
import lombok.NoArgsConstructor;

@Getter
@NoArgsConstructor(access = AccessLevel.PROTECTED)
@Entity
@Table(
    name = "ledger_entries",
    uniqueConstraints = @UniqueConstraint(
        name = "ledger_entries_transfer_id_type_key",
        columnNames = {"transfer_id", "type"}))
public class LedgerEntry {

    @Id
    @GeneratedValue(strategy = GenerationType.IDENTITY)
    @Column(name = "id", nullable = false, updatable = false)
    private Long id;

    @Column(name = "wallet_id", nullable = false, updatable = false)
    private String walletId;

    @Column(name = "transfer_id", nullable = false, updatable = false)
    private UUID transferId;

    @Enumerated(EnumType.STRING)
    @Column(name = "type", nullable = false, length = 8, updatable = false)
    private EntryType type;

    @Column(name = "amount", nullable = false, updatable = false)
    private long amount;

    @CreationTimestamp
    @Column(name = "created_at", nullable = false, updatable = false)
    private Instant createdAt;

    public LedgerEntry(final String walletId,
                       final UUID transferId,
                       final EntryType type,
                       final long amount) {
        if (walletId == null || transferId == null || type == null) {
            throw new IllegalArgumentException("walletId, transferId, type must not be null");
        }
        if (walletId.isBlank()) {
            throw new IllegalArgumentException("walletId must not be blank");
        }
        if (amount <= 0) {
            throw new IllegalArgumentException("amount must be positive");
        }
        this.walletId = walletId;
        this.transferId = transferId;
        this.type = type;
        this.amount = amount;
    }

    public long signedAmount() {
        return type == EntryType.CREDIT ? amount : -amount;
    }
}

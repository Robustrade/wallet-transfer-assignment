package com.walletservice.domain.entity;

import jakarta.persistence.*;
import lombok.*;

import java.time.LocalDateTime;
import java.util.UUID;

/**
 * LedgerEntry entity representing a debit or credit in the double-entry ledger.
 *
 * Design notes:
 * - Ledger entries are immutable; they represent historical facts
 * - Every transfer must create exactly two entries: one DEBIT and one CREDIT
 * - Entry type is an enum to prevent invalid combinations
 * - Ledger entries are created atomically with transfer status updates
 */
@Entity
@Table(name = "ledger_entries")
@Getter
@Setter
@NoArgsConstructor
@AllArgsConstructor
@Builder
public class LedgerEntry {
    @Id
    private String id;

    @Column(name = "transfer_id", nullable = false)
    private String transferId;

    @Column(name = "wallet_id", nullable = false)
    private String walletId;

    @Enumerated(EnumType.STRING)
    @Column(name = "entry_type", nullable = false)
    private EntryType entryType;

    @Column(nullable = false)
    private Long amount;

    @Column(nullable = false, updatable = false)
    private LocalDateTime createdAt;

    @PrePersist
    protected void onCreate() {
        this.id = UUID.randomUUID().toString();
        this.createdAt = LocalDateTime.now();
    }

    public enum EntryType {
        DEBIT,
        CREDIT
    }
}


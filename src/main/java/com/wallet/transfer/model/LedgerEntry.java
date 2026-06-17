package com.wallet.transfer.model;

import jakarta.persistence.*;
import lombok.*;

import java.math.BigDecimal;
import java.time.LocalDateTime;

/**
 * Entity class representing a double-entry ledger record.
 * For every successful transfer, two ledger entries must be created:
 * one DEBIT entry for the source wallet and one CREDIT entry for the destination wallet.
 * This guarantees transaction integrity and auditing consistency.
 */
@Entity
@Table(name = "ledger_entries")
@Getter
@Setter
@NoArgsConstructor
@AllArgsConstructor
@Builder
public class LedgerEntry {

    /**
     * Unique identifier for the ledger entry.
     */
    @Id
    @Column(name = "id", length = 50)
    private String id;

    /**
     * ID of the wallet associated with this entry.
     */
    @Column(name = "wallet_id", nullable = false, length = 50)
    private String walletId;

    /**
     * ID of the parent transfer transaction.
     */
    @Column(name = "transfer_id", nullable = false, length = 50)
    private String transferId;

    /**
     * The type of entry (DEBIT or CREDIT).
     */
    @Enumerated(EnumType.STRING)
    @Column(name = "type", nullable = false, length = 10)
    private LedgerEntryType type;

    /**
     * The change amount associated with this entry.
     */
    @Column(name = "amount", nullable = false, precision = 18, scale = 4)
    private BigDecimal amount;

    /**
     * Timestamp indicating when this ledger entry was written.
     */
    @Column(name = "created_at", nullable = false, updatable = false)
    @Builder.Default
    private LocalDateTime createdAt = LocalDateTime.now();
}

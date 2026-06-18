package com.wallet.transfer.domain;

import jakarta.persistence.*;
import lombok.*;
import java.math.BigDecimal;
import java.time.LocalDateTime;
import java.util.UUID;

/**
 * Ledger Entry Entity
 * 
 * Represents a single entry in the double-entry ledger.
 * 
 * Key Design Decisions:
 * - Immutable (no updates allowed, only inserts)
 * - entry_id: Unique identifier for audit purposes
 * - entry_type: DEBIT or CREDIT
 * - Every transfer must produce exactly 2 entries (1 DEBIT + 1 CREDIT)
 * - Invariant: Sum of all DEBIT entries = Sum of all CREDIT entries
 */
@Entity
@Table(
    name = "ledger_entries",
    indexes = {
        @Index(name = "idx_ledger_entries_entry_id", columnList = "entry_id"),
        @Index(name = "idx_ledger_entries_transfer_id", columnList = "transfer_id"),
        @Index(name = "idx_ledger_entries_wallet_id", columnList = "wallet_id"),
        @Index(name = "idx_ledger_entries_entry_type", columnList = "entry_type"),
        @Index(name = "idx_ledger_entries_created_at", columnList = "created_at")
    },
    uniqueConstraints = {
        @UniqueConstraint(name = "uc_ledger_entries_entry_id", columnNames = "entry_id")
    }
)
@Data
@NoArgsConstructor
@AllArgsConstructor
@Builder
public class LedgerEntry {
    
    @Id
    @GeneratedValue(strategy = GenerationType.UUID)
    private UUID id;
    
    @Column(nullable = false, unique = true, length = 255)
    private String entryId;
    
    @Column(nullable = false, length = 255)
    private String transferId;
    
    @Column(nullable = false, length = 255)
    private String walletId;
    
    @Enumerated(EnumType.STRING)
    @Column(nullable = false, length = 32)
    private LedgerEntryType entryType;
    
    @Column(nullable = false, precision = 19, scale = 2)
    private BigDecimal amount;
    
    @Column(nullable = false, updatable = false)
    private LocalDateTime createdAt;
    
    @PrePersist
    protected void onCreate() {
        createdAt = LocalDateTime.now();
    }
    
    /**
     * Validates ledger entry invariants
     * @throws IllegalArgumentException if validation fails
     */
    public void validate() {
        if (amount.compareTo(BigDecimal.ZERO) <= 0) {
            throw new IllegalArgumentException("Ledger entry amount must be positive");
        }
        if (walletId == null || walletId.trim().isEmpty()) {
            throw new IllegalArgumentException("Wallet ID cannot be empty");
        }
        if (transferId == null || transferId.trim().isEmpty()) {
            throw new IllegalArgumentException("Transfer ID cannot be empty");
        }
        if (entryType == null) {
            throw new IllegalArgumentException("Entry type must be specified");
        }
    }
}

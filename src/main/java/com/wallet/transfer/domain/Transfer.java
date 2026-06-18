package com.wallet.transfer.domain;

import jakarta.persistence.*;
import lombok.*;
import java.math.BigDecimal;
import java.time.LocalDateTime;
import java.util.UUID;

/**
 * Transfer Entity
 * 
 * Represents a wallet-to-wallet transfer transaction.
 * 
 * Key Design Decisions:
 * - id: Technical surrogate key (UUID)
 * - transferId: Business key (unique external identifier)
 * - idempotencyKey: UNIQUE constraint ensures exactly-once semantics
 * - status: Tracks transfer lifecycle (PENDING -> PROCESSED/FAILED)
 * - All fields immutable after creation (except status)
 */
@Entity
@Table(
    name = "transfers",
    indexes = {
        @Index(name = "idx_transfers_transfer_id", columnList = "transfer_id"),
        @Index(name = "idx_transfers_idempotency_key", columnList = "idempotency_key"),
        @Index(name = "idx_transfers_status", columnList = "status"),
        @Index(name = "idx_transfers_from_wallet", columnList = "from_wallet_id"),
        @Index(name = "idx_transfers_to_wallet", columnList = "to_wallet_id"),
        @Index(name = "idx_transfers_created_at", columnList = "created_at")
    },
    uniqueConstraints = {
        @UniqueConstraint(name = "uc_transfers_transfer_id", columnNames = "transfer_id"),
        @UniqueConstraint(name = "uc_transfers_idempotency_key", columnNames = "idempotency_key")
    }
)
@Data
@NoArgsConstructor
@AllArgsConstructor
@Builder
public class Transfer {
    
    @Id
    @GeneratedValue(strategy = GenerationType.UUID)
    private UUID id;
    
    @Column(nullable = false, unique = true, length = 255)
    private String transferId;
    
    @Column(nullable = false, length = 255)
    private String fromWalletId;
    
    @Column(nullable = false, length = 255)
    private String toWalletId;
    
    @Column(nullable = false, precision = 19, scale = 2)
    private BigDecimal amount;
    
    @Enumerated(EnumType.STRING)
    @Column(nullable = false, length = 32)
    private TransferStatus status;
    
    @Column(nullable = false, unique = true, length = 255)
    private String idempotencyKey;
    
    @Column(nullable = false, updatable = false)
    private LocalDateTime createdAt;
    
    @Column(nullable = false)
    private LocalDateTime updatedAt;
    
    @PrePersist
    protected void onCreate() {
        createdAt = LocalDateTime.now();
        updatedAt = LocalDateTime.now();
        if (status == null) {
            status = TransferStatus.PENDING;
        }
    }
    
    @PreUpdate
    protected void onUpdate() {
        updatedAt = LocalDateTime.now();
    }
    
    /**
     * Validates transfer invariants
     * @throws IllegalArgumentException if validation fails
     */
    public void validate() {
        if (fromWalletId.equals(toWalletId)) {
            throw new IllegalArgumentException("Source and destination wallet IDs cannot be the same");
        }
        if (amount.compareTo(BigDecimal.ZERO) <= 0) {
            throw new IllegalArgumentException("Transfer amount must be positive");
        }
        if (fromWalletId == null || fromWalletId.trim().isEmpty()) {
            throw new IllegalArgumentException("Source wallet ID cannot be empty");
        }
        if (toWalletId == null || toWalletId.trim().isEmpty()) {
            throw new IllegalArgumentException("Destination wallet ID cannot be empty");
        }
    }
    
    /**
     * Transitions transfer to PROCESSED state
     * Valid only from PENDING state
     */
    public void markProcessed() {
        if (status != TransferStatus.PENDING) {
            throw new IllegalStateException("Cannot mark transfer as processed from state: " + status);
        }
        this.status = TransferStatus.PROCESSED;
    }
    
    /**
     * Transitions transfer to FAILED state
     * Valid only from PENDING state
     */
    public void markFailed() {
        if (status != TransferStatus.PENDING) {
            throw new IllegalStateException("Cannot mark transfer as failed from state: " + status);
        }
        this.status = TransferStatus.FAILED;
    }
}

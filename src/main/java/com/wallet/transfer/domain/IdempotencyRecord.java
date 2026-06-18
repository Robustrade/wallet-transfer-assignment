package com.wallet.transfer.domain;

import jakarta.persistence.*;
import lombok.*;
import java.time.LocalDateTime;
import java.util.UUID;

/**
 * Idempotency Record Entity
 * 
 * Stores the original response for duplicate requests (retry-safe behavior).
 * 
 * Key Design Decisions:
 * - idempotencyKey: UNIQUE constraint ensures one record per key
 * - Caches the original HTTP response (status + body)
 * - Enables exact replay for duplicate requests
 * - Immutable (no updates allowed)
 */
@Entity
@Table(
    name = "idempotency_records",
    indexes = {
        @Index(name = "idx_idempotency_records_key", columnList = "idempotency_key")
    },
    uniqueConstraints = {
        @UniqueConstraint(name = "uc_idempotency_records_key", columnNames = "idempotency_key")
    }
)
@Data
@NoArgsConstructor
@AllArgsConstructor
@Builder
public class IdempotencyRecord {
    
    @Id
    @GeneratedValue(strategy = GenerationType.UUID)
    private UUID id;
    
    @Column(nullable = false, unique = true, length = 255)
    private String idempotencyKey;
    
    @Column(nullable = false, length = 255)
    private String transferId;
    
    @Column(nullable = false)
    private Integer responseStatusCode;
    
    @Column(columnDefinition = "TEXT")
    private String responseBody;
    
    @Column(nullable = false, updatable = false)
    private LocalDateTime createdAt;
    
    @PrePersist
    protected void onCreate() {
        createdAt = LocalDateTime.now();
    }
    
    /**
     * Validates idempotency record
     * @throws IllegalArgumentException if validation fails
     */
    public void validate() {
        if (idempotencyKey == null || idempotencyKey.trim().isEmpty()) {
            throw new IllegalArgumentException("Idempotency key cannot be empty");
        }
        if (transferId == null || transferId.trim().isEmpty()) {
            throw new IllegalArgumentException("Transfer ID cannot be empty");
        }
        if (responseStatusCode == null || responseStatusCode < 100 || responseStatusCode > 599) {
            throw new IllegalArgumentException("Invalid HTTP status code");
        }
    }
}

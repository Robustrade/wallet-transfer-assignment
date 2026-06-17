package com.wallet.transfer.model;

import jakarta.persistence.*;
import lombok.*;

import java.math.BigDecimal;
import java.time.LocalDateTime;

/**
 * Represents a user's digital wallet in the system.
 * Contains information about the wallet's unique identifier, current balance,
 * and creation timestamp.
 */
@Entity
@Table(name = "wallets")
@Getter
@Setter
@NoArgsConstructor
@AllArgsConstructor
@Builder
public class Wallet {

    /**
     * Unique identifier for the wallet (e.g., UUID or account number).
     */
    @Id
    @Column(name = "id", length = 50)
    private String id;

    /**
     * The current monetary balance in the wallet.
     * Stored with precision 18 and scale 4 to prevent rounding errors.
     */
    @Column(name = "balance", nullable = false, precision = 18, scale = 4)
    private BigDecimal balance;

    /**
     * Timestamp indicating when the wallet was created.
     */
    @Column(name = "created_at", nullable = false, updatable = false)
    @Builder.Default
    private LocalDateTime createdAt = LocalDateTime.now();
}

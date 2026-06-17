package com.wallet.transfer.model;

import jakarta.persistence.*;
import lombok.*;

import java.math.BigDecimal;
import java.time.LocalDateTime;

/**
 * Entity class representing a logged transfer between two wallets.
 * This tracks the request, from/to wallets, amount, and the outcome status of the transaction.
 */
@Entity
@Table(name = "transfers")
@Getter
@Setter
@NoArgsConstructor
@AllArgsConstructor
@Builder
public class Transfer {

    /**
     * Unique identifier for the transfer record.
     */
    @Id
    @Column(name = "id", length = 50)
    private String id;

    /**
     * The unique idempotency key associated with the transfer request.
     * Guaranteed to be unique within the transfers table.
     */
    @Column(name = "idempotency_key", nullable = false, unique = true, length = 100)
    private String idempotencyKey;

    /**
     * The unique ID of the source wallet.
     */
    @Column(name = "from_wallet_id", nullable = false, length = 50)
    private String fromWalletId;

    /**
     * The unique ID of the destination wallet.
     */
    @Column(name = "to_wallet_id", nullable = false, length = 50)
    private String toWalletId;

    /**
     * The transfer amount.
     */
    @Column(name = "amount", nullable = false, precision = 18, scale = 4)
    private BigDecimal amount;

    /**
     * The current execution status of this transfer (e.g., PENDING, PROCESSED, FAILED).
     */
    @Enumerated(EnumType.STRING)
    @Column(name = "status", nullable = false, length = 20)
    private TransferStatus status;

    /**
     * Timestamp indicating when this transfer record was created.
     */
    @Column(name = "created_at", nullable = false, updatable = false)
    @Builder.Default
    private LocalDateTime createdAt = LocalDateTime.now();
}

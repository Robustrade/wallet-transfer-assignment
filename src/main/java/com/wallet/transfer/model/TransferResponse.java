package com.wallet.transfer.model;

import lombok.*;

import java.math.BigDecimal;
import java.time.LocalDateTime;

/**
 * Data Transfer Object (DTO) representing a successful wallet-to-wallet transfer response.
 * Sent back to the client as JSON payload.
 */
@Getter
@Setter
@NoArgsConstructor
@AllArgsConstructor
@Builder
public class TransferResponse {

    /**
     * Unique identifier generated for the transaction.
     */
    private String id;

    /**
     * Unique idempotency key linked with the initial request.
     */
    private String idempotencyKey;

    /**
     * ID of the sender's wallet.
     */
    private String fromWalletId;

    /**
     * ID of the recipient's wallet.
     */
    private String toWalletId;

    /**
     * The amount of money transferred.
     */
    private BigDecimal amount;

    /**
     * The processing status of the transfer (e.g., PROCESSED, FAILED).
     */
    private String status;

    /**
     * Timestamp indicating when the transfer was recorded.
     */
    private LocalDateTime createdAt;
}

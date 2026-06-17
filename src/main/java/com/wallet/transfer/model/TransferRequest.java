package com.wallet.transfer.model;

import jakarta.validation.constraints.DecimalMin;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import lombok.*;

import java.math.BigDecimal;

/**
 * Data Transfer Object (DTO) representing a wallet-to-wallet transfer request.
 * Contains necessary validation constraints to ensure input completeness.
 */
@Getter
@Setter
@NoArgsConstructor
@AllArgsConstructor
@Builder
public class TransferRequest {

    /**
     * Unique key identifying the request to ensure idempotency.
     * Prevents duplicate transactions if the client retries the request.
     */
    @NotBlank(message = "Idempotency key is required")
    private String idempotencyKey;

    /**
     * Unique identifier of the source wallet from which funds will be deducted.
     */
    @NotBlank(message = "Source wallet ID is required")
    private String fromWalletId;

    /**
     * Unique identifier of the destination wallet to which funds will be credited.
     */
    @NotBlank(message = "Destination wallet ID is required")
    private String toWalletId;

    /**
     * The amount of money to be transferred.
     * Must be greater than or equal to 0.0001.
     */
    @NotNull(message = "Amount is required")
    @DecimalMin(value = "0.0001", message = "Amount must be greater than zero")
    private BigDecimal amount;
}

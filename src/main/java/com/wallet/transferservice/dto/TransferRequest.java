package com.wallet.transferservice.dto;

import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.Pattern;

/**
 * Incoming transfer request. All fields are strings — validated and parsed
 * into domain types by the service layer.
 *
 * @param idempotencyKey Client-generated UUIDv4 for idempotency
 * @param fromWalletId   Source wallet UUID
 * @param toWalletId     Destination wallet UUID
 * @param amount         Positive decimal string with up to 2 decimal places
 */
public record TransferRequest(

        @NotBlank(message = "idempotencyKey is required")
        @Pattern(
                regexp = "^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$",
                message = "idempotencyKey must be a valid UUID"
        )
        String idempotencyKey,

        @NotBlank(message = "fromWalletId is required")
        @Pattern(
                regexp = "^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$",
                message = "fromWalletId must be a valid UUID"
        )
        String fromWalletId,

        @NotBlank(message = "toWalletId is required")
        @Pattern(
                regexp = "^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$",
                message = "toWalletId must be a valid UUID"
        )
        String toWalletId,

        @NotBlank(message = "amount is required")
        @Pattern(
                regexp = "^\\d+(\\.\\d{1,2})?$",
                message = "amount must be a positive decimal with up to 2 decimal places"
        )
        String amount
) {
}

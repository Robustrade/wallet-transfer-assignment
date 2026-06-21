package com.wallet.transfer.dto;

import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Pattern;
import jakarta.validation.constraints.Positive;
import jakarta.validation.constraints.Size;

public record TransferRequest(
        @NotBlank(message = "idempotencyKey is required")
        @Size(max = 128, message = "idempotencyKey must not exceed 128 characters")
        @Pattern(regexp = "^[a-zA-Z0-9_\\-.:]+$", message = "idempotencyKey contains invalid characters")
        String idempotencyKey,

        @NotBlank(message = "fromWalletId is required")
        @Size(max = 64, message = "fromWalletId must not exceed 64 characters")
        @Pattern(regexp = "^[a-zA-Z0-9_-]+$", message = "fromWalletId contains invalid characters")
        String fromWalletId,

        @NotBlank(message = "toWalletId is required")
        @Size(max = 64, message = "toWalletId must not exceed 64 characters")
        @Pattern(regexp = "^[a-zA-Z0-9_-]+$", message = "toWalletId contains invalid characters")
        String toWalletId,

        @NotNull(message = "amount is required")
        @Positive(message = "amount must be positive")
        Long amount
) {
}

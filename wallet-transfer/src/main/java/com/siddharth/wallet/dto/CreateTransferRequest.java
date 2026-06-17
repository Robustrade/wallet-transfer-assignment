package com.siddharth.wallet.dto;

import jakarta.validation.constraints.DecimalMin;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;

import java.math.BigDecimal;
import java.util.UUID;

public record CreateTransferRequest(

        @NotBlank(message = "Idempotency key is required")
        String idempotencyKey,

        @NotNull(message = "Source wallet is required")
        UUID fromWalletId,

        @NotNull(message = "Destination wallet is required")
        UUID toWalletId,

        @NotNull(message = "Amount is required")
        @DecimalMin(value = "0.01", message = "Amount must be greater than zero")
        BigDecimal amount
) {
}
package com.wallet.handler;

import java.math.BigDecimal;

import jakarta.validation.constraints.DecimalMin;
import jakarta.validation.constraints.Digits;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;

public record TransferRequest(
        @NotBlank String idempotencyKey,
        @NotBlank String fromWalletId,
        @NotBlank String toWalletId,
        @NotNull
        @DecimalMin(value = "0.0", inclusive = false)
        @Digits(integer = 15, fraction = 4) BigDecimal amount) {
}

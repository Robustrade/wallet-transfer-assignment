package com.wallet.dto;

import jakarta.validation.constraints.DecimalMin;
import jakarta.validation.constraints.Digits;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Size;

import java.math.BigDecimal;

public record CreateTransferRequest(
        @NotBlank @Size(max = 255) String idempotencyKey,
        @NotBlank @Size(max = 64) String fromWalletId,
        @NotBlank @Size(max = 64) String toWalletId,
        @NotNull @DecimalMin(value = "0.0", inclusive = false) @Digits(integer = 15, fraction = 4) BigDecimal amount
) {
}

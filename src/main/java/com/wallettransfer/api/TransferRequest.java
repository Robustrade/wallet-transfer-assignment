package com.wallettransfer.api;

import jakarta.validation.constraints.DecimalMin;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Size;

import java.math.BigDecimal;

public record TransferRequest(
        @NotBlank @Size(max = 255) String idempotencyKey,
        @NotBlank @Size(max = 100) String fromWalletId,
        @NotBlank @Size(max = 100) String toWalletId,
        @NotNull @DecimalMin(value = "0.0001") BigDecimal amount) {
}
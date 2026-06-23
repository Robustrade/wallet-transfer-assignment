package com.example.wallet.web.dto;

import jakarta.validation.constraints.DecimalMin;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import java.math.BigDecimal;

public record TransferRequest(
        @NotBlank String idempotencyKey,
        @NotBlank String fromWalletId,
        @NotBlank String toWalletId,
        @NotNull @DecimalMin(value = "0.0001", message = "amount must be greater than zero") BigDecimal amount) {}

package com.api.wallet.dto;

import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Positive;
import java.math.BigDecimal;

public record CreateTransferRequest(
        @NotBlank String idempotencyKey,
        @NotBlank String fromWalletId,
        @NotBlank String toWalletId,
        @NotNull @Positive BigDecimal amount) {}

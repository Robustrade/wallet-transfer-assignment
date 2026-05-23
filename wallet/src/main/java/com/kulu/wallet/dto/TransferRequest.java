package com.kulu.wallet.dto;

import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Positive;

public record TransferRequest(
    @NotBlank String idempotencyKey,
    @NotNull Long fromWalletId,
    @NotNull Long toWalletId,
    @NotNull @Positive Long amount
) {}

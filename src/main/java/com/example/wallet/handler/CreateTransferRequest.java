package com.example.wallet.handler;

import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Positive;
import jakarta.validation.constraints.Size;

/** Amount is in minor currency units (e.g. paise/cents). */
public record CreateTransferRequest(
        @NotBlank @Size(max = 128) String idempotencyKey,
        @NotBlank @Size(max = 64) String fromWalletId,
        @NotBlank @Size(max = 64) String toWalletId,
        @NotNull @Positive Long amount) {}

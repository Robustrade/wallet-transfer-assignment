package com.wallet.api.dto;

import com.wallet.domain.TransferStatus;
import jakarta.validation.constraints.Min;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;

public record CreateTransferRequest(
        @NotBlank String idempotencyKey,
        @NotBlank String fromWalletId,
        @NotBlank String toWalletId,
        @NotNull @Min(1) Long amount) {}

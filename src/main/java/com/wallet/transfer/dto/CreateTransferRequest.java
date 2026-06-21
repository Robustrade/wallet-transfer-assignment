package com.wallet.transfer.dto;

import jakarta.validation.constraints.Min;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;

public record CreateTransferRequest(
    String idempotencyKey,

    @NotBlank(message = "fromWalletId must not be blank")
    String fromWalletId,

    @NotBlank(message = "toWalletId must not be blank")
    String toWalletId,

    @NotNull(message = "amount must not be null")
    @Min(value = 1, message = "amount must be greater than 0")
    Long amount
) {}

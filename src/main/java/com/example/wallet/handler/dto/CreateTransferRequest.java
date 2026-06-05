package com.example.wallet.handler.dto;

import com.example.wallet.handler.dto.validation.NotSelfTransfer;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Positive;

import java.math.BigDecimal;

@NotSelfTransfer
public record CreateTransferRequest(

        @NotBlank(message = "idempotencyKey must not be blank")
        String idempotencyKey,

        @NotBlank(message = "fromWalletId must not be blank")
        String fromWalletId,

        @NotBlank(message = "toWalletId must not be blank")
        String toWalletId,

        @NotNull(message = "amount must not be null")
        @Positive(message = "amount must be positive")
        BigDecimal amount
) {}

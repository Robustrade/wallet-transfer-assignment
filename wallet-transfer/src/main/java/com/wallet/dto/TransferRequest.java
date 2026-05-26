package com.wallet.dto;

import jakarta.validation.constraints.DecimalMin;
import jakarta.validation.constraints.Digits;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import java.math.BigDecimal;

public record TransferRequest(

    @NotBlank(message = "idempotencyKey must not be blank")
    String idempotencyKey,

    @NotBlank(message = "fromWalletId must not be blank")
    String fromWalletId,

    @NotBlank(message = "toWalletId must not be blank")
    String toWalletId,

    @NotNull(message = "amount must not be null")
    @DecimalMin(value = "0.0001", message = "amount must be greater than zero")
    @Digits(integer = 15, fraction = 4, message = "amount must have at most 15 integer digits and 4 decimal places")
    BigDecimal amount
) {}

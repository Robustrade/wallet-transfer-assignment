package com.assignment.wallet_transfer.dtos;

import jakarta.validation.constraints.DecimalMin;
import jakarta.validation.constraints.NotNull;

import java.math.BigDecimal;
import java.util.UUID;

public record TransferRequest(

        @NotNull
        UUID sourceWalletId,

        @NotNull
        UUID destinationWalletId,

        @NotNull
        @DecimalMin("0.01")
        BigDecimal amount
) {

}
package com.example.wallet_transfer_assignment.dto;

import jakarta.validation.constraints.DecimalMin;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;

import java.math.BigDecimal;

public record CreateWalletRequest(

        @NotBlank
        String ownerName,

        @NotNull
        @DecimalMin(value = "0.0")
        BigDecimal initialBalance
) {
}
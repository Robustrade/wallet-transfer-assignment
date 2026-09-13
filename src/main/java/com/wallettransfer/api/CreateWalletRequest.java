package com.wallettransfer.api;

import jakarta.validation.constraints.DecimalMin;
import jakarta.validation.constraints.NotNull;

import java.math.BigDecimal;

public record CreateWalletRequest(
        @NotNull @DecimalMin("0.00") BigDecimal initialBalance) {
}
package com.example.wallet.handler;

import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.PositiveOrZero;
import jakarta.validation.constraints.Size;

public record CreateWalletRequest(
        @NotBlank @Size(max = 64) String id, @PositiveOrZero long openingBalance) {}

package com.wallettransfer.wallet.dto;

import jakarta.validation.constraints.Min;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Pattern;

public record CreateWalletRequest(
        @NotBlank
        @Pattern(regexp = "^[a-zA-Z0-9_-]+$", message = "Wallet id must be alphanumeric with _ or -")
        String id,
        @NotNull @Min(0) Long initialBalance
) {
}

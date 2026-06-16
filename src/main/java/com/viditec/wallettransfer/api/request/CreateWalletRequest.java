package com.viditec.wallettransfer.api.request;

import jakarta.validation.constraints.Min;
import jakarta.validation.constraints.NotBlank;

public record CreateWalletRequest(
        @NotBlank String walletId,
        @Min(0) long initialBalance
) {
}

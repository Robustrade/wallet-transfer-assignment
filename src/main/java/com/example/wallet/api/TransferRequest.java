package com.example.wallet.api;

import io.swagger.v3.oas.annotations.media.Schema;
import jakarta.validation.constraints.*;

import java.math.BigDecimal;

@Schema(
        description = "Request to transfer funds between two wallets"
)
public record TransferRequest(

        @Schema(
                description = "Unique key used to make the request idempotent",
                example = "transfer-12345"
        )
        @NotBlank
        String idempotencyKey,

        @Schema(
                description = "Wallet from which funds are transferred",
                example = "wallet_1"
        )
        @NotBlank
        String fromWalletId,

        @Schema(
                description = "Wallet receiving the funds",
                example = "wallet_2"
        )
        @NotBlank
        String toWalletId,

        @Schema(
                description = "Amount to transfer",
                example = "100.00"
        )
        @Positive
        BigDecimal amount
) {
}
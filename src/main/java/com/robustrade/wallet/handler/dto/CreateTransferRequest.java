package com.robustrade.wallet.handler.dto;

import jakarta.validation.constraints.AssertTrue;
import jakarta.validation.constraints.DecimalMin;
import jakarta.validation.constraints.Digits;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Size;
import java.math.BigDecimal;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class CreateTransferRequest {

    @NotBlank(message = "idempotencyKey can not be empty")
    @Size(max = 128, message = "idempotencyKey must be at most 128 characters")
    private String idempotencyKey;

    @NotBlank(message = "fromWalletId can not be empty")
    private String fromWalletId;

    @NotBlank(message = "toWalletId can not be empty")
    private String toWalletId;

    @NotNull
    @DecimalMin(value = "0.01", message = "Invalid amount. Amount must be at least 0.01")
    @Digits(
            integer = 13,
            fraction = 2,
            message = "amount must match NUMERIC(15,2) (max 13 digits before decimal, 2 after)"
    )
    private BigDecimal amount;

    @AssertTrue(message = "fromWalletId and toWalletId must be different")
    public boolean isWalletsDistinct() {
        if (fromWalletId == null || toWalletId == null) {
            return true;
        }
        return !fromWalletId.equals(toWalletId);
    }
}

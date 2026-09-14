package com.robustrade.wallet.handler.dto;

import jakarta.validation.constraints.DecimalMin;
import jakarta.validation.constraints.Digits;
import jakarta.validation.constraints.NotNull;
import java.math.BigDecimal;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class CreateWalletRequest {

    @NotNull(message = "userId can not be empty")
    private Long userId;

    @NotNull(message = "balance can not be empty")
    @DecimalMin(value = "0.00", inclusive = true, message = "Invalid balance. Balance must be at least 0.00")
    @Digits(
            integer = 13,
            fraction = 2,
            message = "balance must match NUMERIC(15,2) (max 13 digits before decimal, 2 after)"
    )
    private BigDecimal balance;
}

package com.assignment.wallet_transfer.dtos;

import jakarta.validation.constraints.DecimalMin;
import java.math.BigDecimal;

public record CreateWalletRequest(
        @DecimalMin("0.0")
        BigDecimal balance
) {

}

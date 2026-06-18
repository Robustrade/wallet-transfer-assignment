package com.example.wallet_transfer_assignment.dto;

import java.math.BigDecimal;
import java.util.UUID;

public record BalanceResponse(
        UUID walletId,
        BigDecimal balance
) {
}
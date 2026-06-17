package com.assignment.wallet_transfer.dtos;

import java.math.BigDecimal;
import java.util.UUID;

public record WalletResponse(
        UUID walletId,
        BigDecimal balance
) {

}
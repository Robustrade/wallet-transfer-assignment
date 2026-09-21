package com.example.wallet.api;

import java.math.BigDecimal;

public record WalletBalanceResponse(
        String walletId,
        BigDecimal balance
) {

}
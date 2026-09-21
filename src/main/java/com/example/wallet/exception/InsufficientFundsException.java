package com.example.wallet.exception;

import java.math.BigDecimal;

public class InsufficientFundsException extends BusinessException {

    public InsufficientFundsException(
            String walletId,
            BigDecimal requestedAmount,
            BigDecimal availableBalance) {

        super("Insufficient funds in wallet " + walletId
                + ". Requested: " + requestedAmount
                + ", available: " + availableBalance);
    }
}
package com.wallet.exception;

import com.wallet.domain.Wallet;

import java.math.BigDecimal;

public class InsufficientBalanceException extends RuntimeException{

    public InsufficientBalanceException(String walletId, BigDecimal currentBalance, BigDecimal requestedAmount) {
        super("Wallet " + walletId + " has insufficient balance: balance=" + currentBalance
                + " requested=" + requestedAmount);
    }
}

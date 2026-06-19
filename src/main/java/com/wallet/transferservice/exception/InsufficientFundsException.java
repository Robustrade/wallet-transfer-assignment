package com.wallet.transferservice.exception;

import java.math.BigDecimal;
import java.util.UUID;

public class InsufficientFundsException extends RuntimeException {
    private final UUID walletId;
    private final BigDecimal currentBalance;
    private final BigDecimal requestedAmount;

    public InsufficientFundsException(UUID walletId, BigDecimal currentBalance, BigDecimal requestedAmount) {
        super(String.format("Insufficient funds in wallet %s: balance=%s, requested=%s",
                walletId, currentBalance.toPlainString(), requestedAmount.toPlainString()));
        this.walletId = walletId;
        this.currentBalance = currentBalance;
        this.requestedAmount = requestedAmount;
    }

    public UUID getWalletId() {
        return walletId;
    }

    public BigDecimal getCurrentBalance() {
        return currentBalance;
    }

    public BigDecimal getRequestedAmount() {
        return requestedAmount;
    }
}

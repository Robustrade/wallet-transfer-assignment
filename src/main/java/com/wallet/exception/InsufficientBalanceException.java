package com.wallet.exception;

/**
 * Thrown when a wallet does not have sufficient balance for a debit operation.
 */
public class InsufficientBalanceException extends RuntimeException {

    private final String walletId;
    private final long currentBalance;
    private final long requestedAmount;

    public InsufficientBalanceException(String walletId, long currentBalance, long requestedAmount) {
        super("Insufficient balance in wallet " + walletId
                + ": available=" + currentBalance + ", requested=" + requestedAmount);
        this.walletId = walletId;
        this.currentBalance = currentBalance;
        this.requestedAmount = requestedAmount;
    }

    public String getWalletId() {
        return walletId;
    }

    public long getCurrentBalance() {
        return currentBalance;
    }

    public long getRequestedAmount() {
        return requestedAmount;
    }
}

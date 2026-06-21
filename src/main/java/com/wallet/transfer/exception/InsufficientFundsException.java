package com.wallet.transfer.exception;

public class InsufficientFundsException extends RuntimeException {

    private final String walletId;
    private final Long requestedAmount;

    public InsufficientFundsException(String walletId, Long requestedAmount) {
        super(String.format("Wallet %s has insufficient funds for transfer of %d",
                walletId, requestedAmount));
        this.walletId = walletId;
        this.requestedAmount = requestedAmount;
    }

    public String getWalletId() {
        return walletId;
    }

    public Long getRequestedAmount() {
        return requestedAmount;
    }
}

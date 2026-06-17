package com.wallettransfer.wallet.domain;

public class InsufficientBalanceException extends RuntimeException {

    private final String walletId;
    private final long available;
    private final long requested;

    public InsufficientBalanceException(String walletId, long available, long requested) {
        super("Insufficient balance in wallet " + walletId);
        this.walletId = walletId;
        this.available = available;
        this.requested = requested;
    }

    public String getWalletId() {
        return walletId;
    }

    public long getAvailable() {
        return available;
    }

    public long getRequested() {
        return requested;
    }
}

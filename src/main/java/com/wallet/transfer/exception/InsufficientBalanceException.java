package com.wallet.transfer.exception;

import java.math.BigDecimal;

/**
 * Thrown when a wallet has insufficient balance for a transfer
 */
public class InsufficientBalanceException extends WalletTransferException {
    private final String walletId;
    private final BigDecimal available;
    private final BigDecimal requested;

    public InsufficientBalanceException(String walletId, BigDecimal available, BigDecimal requested) {
        super(String.format("Insufficient balance in wallet %s. Available: %s, Requested: %s", 
              walletId, available, requested));
        this.walletId = walletId;
        this.available = available;
        this.requested = requested;
    }

    public String getWalletId() {
        return walletId;
    }

    public BigDecimal getAvailable() {
        return available;
    }

    public BigDecimal getRequested() {
        return requested;
    }
}

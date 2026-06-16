package com.viditec.wallettransfer.exception;

public class InsufficientFundsException extends TransferException {

    public InsufficientFundsException(String walletId) {
        super("INSUFFICIENT_FUNDS", "Wallet '%s' does not have enough balance.".formatted(walletId));
    }
}

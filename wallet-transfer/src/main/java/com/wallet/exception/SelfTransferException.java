package com.wallet.exception;

public class SelfTransferException extends RuntimeException {

    public SelfTransferException(String walletId) {
        super("Transfer source and destination are the same wallet: " + walletId);
    }
}

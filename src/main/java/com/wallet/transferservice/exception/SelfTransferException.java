package com.wallet.transferservice.exception;

public class SelfTransferException extends RuntimeException {
    public SelfTransferException() {
        super("Cannot transfer to the same wallet.");
    }
}

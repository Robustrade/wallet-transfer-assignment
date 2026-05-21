package com.kullu.wallet.exception;

public class SelfTransferException extends RuntimeException {

    public SelfTransferException() {
        super("from and to wallets must differ");
    }
}

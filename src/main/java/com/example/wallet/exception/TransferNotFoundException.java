package com.example.wallet.exception;

public class TransferNotFoundException extends ApplicationException {

    public TransferNotFoundException(String transferId) {
        super("Transfer not found: " + transferId);
    }
}
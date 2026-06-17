package com.wallet.api.exception;

public class TransferNotFoundException extends RuntimeException {

    private final String transferId;

    public TransferNotFoundException(String transferId) {
        super("Transfer not found: " + transferId);
        this.transferId = transferId;
    }

    public String getTransferId() {
        return transferId;
    }
}

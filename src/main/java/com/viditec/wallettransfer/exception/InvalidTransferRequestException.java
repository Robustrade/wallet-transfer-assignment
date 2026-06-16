package com.viditec.wallettransfer.exception;

public class InvalidTransferRequestException extends TransferException {

    public InvalidTransferRequestException(String message) {
        super("INVALID_TRANSFER_REQUEST", message);
    }
}

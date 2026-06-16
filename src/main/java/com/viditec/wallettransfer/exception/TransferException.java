package com.viditec.wallettransfer.exception;

public abstract class TransferException extends RuntimeException {

    private final String code;

    protected TransferException(String code, String message) {
        super(message);
        this.code = code;
    }

    public String getCode() {
        return code;
    }
}
